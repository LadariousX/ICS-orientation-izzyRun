"""
Loads a checkpoint trained by train.py and plays live on the deployed site.
Same DinoEnv, same state/action code -- just pointed at prod and running
greedy (no exploration) inference instead of training.

Built to be left running indefinitely alongside a training process: it serves
`checkpoints/best.pt` (the one-way "best policy so far" file) and re-reads it
between episodes, so improvements from training show up on the leaderboard
without restarting anything.

Usage:
    python play_prod.py --url https://your-deployed-game.com
"""

import argparse
import time
import torch
import os

# Automatically changes the working directory to the script's actual folder
os.chdir(os.path.dirname(os.path.abspath(__file__)))

from dino_env import DinoEnv, STATE_DIM, N_ACTIONS
from dqn import QNetwork


def resolve_checkpoint(path):
    """Prefer the requested checkpoint, fall back to the legacy rolling one."""
    if os.path.exists(path):
        return path
    if path != "checkpoint.pt" and os.path.exists("checkpoint.pt"):
        print(f"'{path}' not found yet -- falling back to checkpoint.pt")
        return "checkpoint.pt"
    raise FileNotFoundError(
        f"No checkpoint at '{path}' (and no checkpoint.pt fallback). "
        f"Run train.py first.")


def load_weights(path, device, q_net=None):
    """Returns (q_net, mtime). Builds the network on first call, otherwise
    loads new weights into the existing one."""
    mtime = os.path.getmtime(path)
    ckpt = torch.load(path, map_location=device)
    if q_net is None:
        q_net = QNetwork(ckpt["state_dim"], ckpt["n_actions"]).to(device)
    q_net.load_state_dict(ckpt["model_state_dict"])
    q_net.eval()
    metric = ckpt.get("metric")
    metric_str = f", avg score {metric:.1f}" if metric is not None else ""
    print(f"Loaded '{path}' from episode {ckpt['episode']}{metric_str}")
    return q_net, mtime


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True)
    ap.add_argument("--checkpoint", default="checkpoints/best.pt")
    ap.add_argument("--episodes", type=int, default=0,
                     help="0 = play forever")
    ap.add_argument("--step-delay", type=float, default=0.05)
    ap.add_argument("--name", default="Arthur Isaac",
                     help="leaderboard name; one row per name, highest score wins")
    ap.add_argument("--reload-page-every", type=int, default=200,
                     help="rebuild the browser tab every N episodes to bound memory; 0 disables")
    ap.add_argument("--no-hot-reload", dest="hot_reload", action="store_false",
                     help="don't re-read the checkpoint when it changes on disk")
    ap.add_argument("--headless", action="store_true", default=False,
                     help="headless off by default so you can watch it play for the demo")
    args = ap.parse_args()

    device = torch.device("mps") if torch.backends.mps.is_available() else torch.device("cpu")

    path = resolve_checkpoint(args.checkpoint)
    q_net, ckpt_mtime = load_weights(path, device)

    env = DinoEnv(
        url=args.url,
        headless=args.headless,
        step_delay=args.step_delay,
        player_name=args.name,
        disable_score_submit=False,  # let it post real scores on prod
        reload_every=args.reload_page_every,
    )

    ep = 0
    best = 0
    failures = 0
    try:
        while args.episodes == 0 or ep < args.episodes:
            # Pick up a newly promoted policy without a restart. Between
            # episodes only, so weights never change mid-run.
            if args.hot_reload and os.path.exists(args.checkpoint):
                try:
                    if os.path.getmtime(args.checkpoint) != ckpt_mtime:
                        q_net, ckpt_mtime = load_weights(args.checkpoint, device, q_net)
                except Exception as e:
                    print(f"  (checkpoint reload failed, keeping current weights: {e})")

            try:
                state = env.reset()
                done = False
                info = {"score": 0}
                while not done:
                    with torch.no_grad():
                        s = torch.from_numpy(state).unsqueeze(0).to(device)
                        action = int(q_net(s).argmax(dim=1).item())
                    state, reward, done, info = env.step(action)
            except Exception as e:
                failures += 1
                print(f"episode failed ({failures}): {type(e).__name__}: {e}")
                try:
                    env.recycle()
                except Exception:
                    pass
                time.sleep(min(60, 2 ** failures))
                continue

            failures = 0
            ep += 1
            best = max(best, info["score"])
            print(f"episode {ep}: score {info['score']}  (best this session {best})")
    except KeyboardInterrupt:
        print(f"\nStopped after {ep} episodes, best {best}")
    finally:
        env.close()


if __name__ == "__main__":
    main()
