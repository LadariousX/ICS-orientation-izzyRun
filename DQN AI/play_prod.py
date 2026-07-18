"""
Loads a checkpoint trained by train.py and plays live on the deployed site.
Same DinoEnv, same state/action code -- just pointed at prod and running
greedy (no exploration) inference instead of training.

Usage:
    python play_prod.py --url https://your-deployed-game.com --checkpoint checkpoint.pt
"""

import argparse
import torch
import os

# Automatically changes the working directory to the script's actual folder
os.chdir(os.path.dirname(os.path.abspath(__file__)))

from dino_env import DinoEnv, STATE_DIM, N_ACTIONS
from dqn import QNetwork


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True)
    ap.add_argument("--checkpoint", default="checkpoint.pt")
    ap.add_argument("--episodes", type=int, default=5)
    ap.add_argument("--step-delay", type=float, default=0.05)
    ap.add_argument("--headless", action="store_true", default=False,
                     help="headless off by default so you can watch it play for the demo")
    args = ap.parse_args()

    device = torch.device("mps") if torch.backends.mps.is_available() else torch.device("cpu")

    ckpt = torch.load(args.checkpoint, map_location=device)
    q_net = QNetwork(ckpt["state_dim"], ckpt["n_actions"]).to(device)
    q_net.load_state_dict(ckpt["model_state_dict"])
    q_net.eval()
    print(f"Loaded checkpoint from episode {ckpt['episode']}")

    env = DinoEnv(
        url=args.url,
        headless=args.headless,
        step_delay=args.step_delay,
        disable_score_submit=False,  # let it post real scores on prod
    )

    try:
        ep =0
        while True:
            ep = ep + 1
            state = env.reset()
            done = False
            while not done:
                with torch.no_grad():
                    s = torch.from_numpy(state).unsqueeze(0).to(device)
                    action = int(q_net(s).argmax(dim=1).item())
                state, reward, done, info = env.step(action)
            print(f"episode {ep + 1}: score {info['score']}")
    finally:
        env.close()


if __name__ == "__main__":
    main()
