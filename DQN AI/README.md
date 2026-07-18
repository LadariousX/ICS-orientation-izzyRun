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
python train.py --url http://localhost:3000 --num-envs 6
```

This launches **one** Chromium process with 6 isolated browser tabs
(contexts), each running its own episode as an asyncio task, all feeding
one shared replay buffer. A separate trainer thread pulls batches and does
the gradient updates continuously in the background — so training isn't
blocked waiting on any single tab. Checkpoints save to `checkpoint.pt`
every 50 episodes (counted across all envs) and again on Ctrl+C.

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
python3 train.py --url http://localhost:8080/game --show
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
python3 play_prod.py --url https://ics.laydenb.com/game --checkpoint checkpoint.pt
```

This opens a real (headed, so you can show it live) Chrome window against
prod, loads the checkpoint, and plays greedily (no exploration) using the
exact same `dino_env.py` / `window.__rl` code path used in training.

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
