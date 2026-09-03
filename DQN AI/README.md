# Izzy Run RL Agent

Trains a small DQN (pure numpy + torch, no images) to play Izzy Run by
reading real game state through a small hook added to `game.js`, then plays
the deployed version using the same code path.

`game.rl-patched.js` is your `game.js` with one addition: a `window.__rl`
object at the end of the closure exposing `getState()`, `isDone()`,
`getScore()`, `reset()`, and `applyAction(action)`. It also adds one guard
in `die()` so training runs don't spam `/api/scores/submit`.

Diff it against your original and drop it in as your `game.js` (both on
localhost for training and in the deployed build for the demo — same file
in both places, that's the whole point).

## 2. Install deps

```bash
pip install torch playwright numpy matplotlib
playwright install chromium
```

On the M4, torch will pick up the `mps` backend automatically (see
`get_device()` in `train.py`) — no CUDA needed.

## 3. Train

Run your game locally (however you normally serve it), then, for the
overnight run:

```bash
cd dino_rl
python train.py --url http://localhost:8080/ --num-envs 6
```

This launches **one** Chromium process with 6 isolated browser tabs
(contexts), each running its own episode as an asyncio task, all feeding
one shared replay buffer. A separate trainer thread pulls batches and does
the gradient updates continuously in the background — so training isn't
blocked waiting on any single tab. Checkpoints save to `checkpoint.pt`
every 50 episodes (counted across all envs) and again on Ctrl+C.

### The three checkpoint files

Leaving training up indefinitely means the rolling checkpoint can't be the
one you serve, so the roles are split:

| File | Written | Use |
|---|---|---|
| `checkpoint.pt` | every 50 episodes, unconditionally | resume training (latest weights + optimizer + episode count) |
| `checkpoints/best.pt` | only when it improves | **the one to serve** — one-way, a collapse can never overwrite it |
| `checkpoints/score<N>_<ts>.pt` | every 12h | history / manual rollback |

Promotion to `best.pt` is gated on the mean score over the last
`--metric-window` (50) episodes, checked every `--promote-every` (25)
episodes — an average, not a single lucky run. The bar is read back off
`best.pt` itself rather than kept in memory, so restarting training can't
reset it and let a weak policy reclaim the file.

**Archived checkpoints**: on top of the rolling `checkpoint.pt`, every 12
hours of wall-clock training drops a snapshot into `checkpoints/`, named
with the best single-episode score seen during that window — e.g.
`checkpoints/score1473_20260829-2332.pt`. Score leads the filename so
`ls checkpoints/` sorts by how good the run got; the timestamp keeps two
equally-good windows from colliding. The window's high-score counter
resets after each archive, so each file's number describes only its own
12 hours, not the run's all-time best. Tune with `--archive-every-hours`
(`0` disables it) and `--checkpoint-dir`. The archive fires on the first
episode to *finish* after the 12-hour mark, so with long episodes expect
it a little late rather than exactly on the hour.

**Resuming is automatic**: every run of `train.py` loads `checkpoint.pt`
if it exists (model weights, optimizer state, and episode count, so the
epsilon decay schedule picks up where it left off) before doing anything
else. Pass `--fresh` if you actually want to discard the existing
checkpoint and start over. Note the replay buffer itself isn't
saved/restored — a resumed run refills its buffer over the first
`--batch-size` or so steps before training kicks back in, which is
negligible next to a day-long run.

Tune `--num-envs` to your M4: more tabs = more experience per wall-clock
second, but each is a real Chromium renderer, so watch Activity Monitor
and back off if things get memory-bound. 4–8 is a reasonable starting
range.

To sanity-check the agent visually instead of training in bulk:

```bash
python3 train.py --url http://localhost:8080/ --show
```

This runs a single headed browser tab plus a live heatmap of each layer's
weight matrix (`10→128`, `128→128`, `128→3`), updating as training
progresses — useful for watching the network actually learn (weights
drifting away from their random initialization, patterns emerging) rather
than just staring at score. `--show` forces `--num-envs 1` since
matplotlib isn't thread-safe to drive from multiple workers.

Unlike parallel training, `--show` submits its scores to
`/api/scores/submit` the same way a real player would — it's meant to be
run against the deployed URL as a live, watchable session, and the
leaderboard entry reflects that. Training keeps happening in the
background on every step exactly as in parallel mode; this only changes
whether the score gets posted, not whether the network learns. Point
`--show` at localhost instead if you'd rather it not touch any real
leaderboard.

A few things worth watching the first few minutes either way:

- Confirm `avg20` (rolling average score) is trending up, not flat.
- If episodes are very short (agent dies almost instantly) for a long
  time, that's normal for the first few hundred episodes of DQN with high
  epsilon — it's still mostly exploring.

For an M4 with a handful of parallel tabs at ~50ms steps, expect somewhere
in the range of several thousand episodes over a day — plenty for this
size of state/action space.

## 4. Play on the deployed site (for the demo)

```bash
python3 play_prod.py --url https://ics.laydenb.com/
```

This opens a real (headed, so you can show it live) Chrome window against
prod, loads `checkpoints/best.pt` (falling back to `checkpoint.pt` if you
haven't trained long enough to promote one yet), and plays greedily (no
exploration) using the exact same `dino_env.py` / `window.__rl` code path
used in training.

It plays forever by default and posts every score. The leaderboard is one
row per name keyed on the lowercased name with the highest score winning
(`app/scores.go`), so a permanently-running bot ratchets a single row
upward instead of flooding the board.

Between episodes it re-checks `best.pt`'s mtime and reloads the weights if
training has promoted a new policy — so the deployed bot gets better on
its own without a restart. `--no-hot-reload` pins it to the weights it
started with; `--name` changes the leaderboard row it occupies.

## Leaving it running indefinitely

```bash
# train forever against localhost (not prod -- 6 tabs is real load)
python3 train.py --url http://localhost:8080/ --num-envs 6

# and, separately, the bot that holds its spot on the board
python3 play_prod.py --url https://ics.laydenb.com/
```

What makes this survivable unattended:

- **`best.pt` only moves forward** — the deployed bot's policy cannot
  regress, even though training itself will dip and occasionally collapse.
- **`--eps-end` defaults to `0.1`** (not `0.05`). On an indefinite run
  epsilon spends essentially all its life at the floor, and a policy that
  stops exploring ends up training only on its own recent behavior.
- **Tabs rebuild every `--reload-page-every` (200) episodes.** A page
  driven for days by `__rl.reset()` alone accumulates JS heap.
- **Failures don't end the run.** A wedged tab is recycled with backoff; a
  worker that fails `--max-worker-failures` (5) episodes in a row hands off
  to a supervisor that relaunches the whole Playwright stack, keeping the
  model, optimizer, buffer and promotion bar intact. `--no-supervise`
  restores exit-on-crash.
- **The trainer thread catches its own errors.** If it died silently the
  tabs would keep playing forever while nothing learned — a failure that
  looks exactly like a healthy run.
- **`shared.scores` is a bounded deque**, not a list that grows for weeks.

### What this does *not* fix

Training longer is not monotonic improvement, and no flag changes that.
`dqn.py` uses a plain `target_net(...).max()` (no Double-DQN decoupling),
so Q-value overestimation still compounds; and with a 50k buffer at ~90
steps/sec across 6 envs, the replay memory only ever spans ~10 minutes of
very recent, highly-correlated experience. Expect the training curve to
plateau and thrash. The guarantee here is about the *served* model, not the
learner: `best.pt` keeps whatever the best policy was, so thrashing costs
you compute rather than the demo. Check in on it every week or so rather
than trusting it for a semester.

## Tuning notes

- **Reward shaping** lives in `dino_env.py: DinoEnv.step()`. Current
  reward is `0.1` survival bonus per step + `0.1` per score-point gained,
  `-1.0` on death. If the agent converges on an overly cautious or overly
  reckless policy, this is the first thing to adjust.
- **State normalization** (`STATE_SCALE` in `dino_env.py`) is a rough
  guess based on the constants in `game.js` (jump velocity, obstacle
  sizes). Print a few raw `getState()` calls during a manual playthrough
  and adjust if values are clipping or too small.
- **Two visible obstacles** are included in the state (`dist0`/`dist1`) so
  the agent has some look-ahead for back-to-back obstacles. If you added a
  third obstacle slot in-game, extend `getState()` and `STATE_DIM` (both
  `game.rl-patched.js` and `dino_env.py`) to match.
