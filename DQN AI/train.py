"""
Train the DQN agent against the game running on localhost.

Two modes:

  Headless, parallel (default) -- N browser tabs run concurrently in worker
  threads, all feeding one shared replay buffer, with a dedicated trainer
  thread doing the gradient updates. This is the one to leave running
  overnight on the M4.

      python train.py --url http://localhost:3000 --num-envs 6

  Visible, single env (--show) -- one headed browser window plus a live
  matplotlib heatmap of each layer's weight matrix, updating as training
  progresses. Useful for watching the network actually change, not for
  bulk training.

      python train.py --url http://localhost:3000 --show

--show forces --num-envs 1 (matplotlib isn't thread-safe to drive from
multiple workers, and you only want to watch one tab at a time anyway).
"""

import argparse
import asyncio
import threading
import time
import numpy as np
import torch
from playwright.async_api import async_playwright
import os

# Automatically changes the working directory to the script's actual folder
os.chdir(os.path.dirname(os.path.abspath(__file__)))

from dino_env import AsyncDinoEnv, DinoEnv, STATE_DIM, N_ACTIONS
from dqn import QNetwork, ReplayBuffer, select_action, train_step


def get_device():
    if torch.backends.mps.is_available():
        return torch.device("mps")
    if torch.cuda.is_available():
        return torch.device("cuda")
    return torch.device("cpu")


def compute_epsilon(episode, args):
    return max(
        args.eps_end,
        args.eps_start - (args.eps_start - args.eps_end) * (episode / args.eps_decay_episodes),
    )


def save_checkpoint(path, q_net, optimizer, episode):
    torch.save({
        "model_state_dict": q_net.state_dict(),
        "optimizer_state_dict": optimizer.state_dict(),
        "episode": episode,
        "state_dim": STATE_DIM,
        "n_actions": N_ACTIONS,
    }, path)


def load_checkpoint_into(path, q_net, target_net, optimizer, device):
    """Returns the episode count to resume from (0 if no checkpoint / --fresh)."""
    import os
    if not os.path.exists(path):
        print(f"No checkpoint found at '{path}' -- starting fresh")
        return 0

    ckpt = torch.load(path, map_location=device)
    q_net.load_state_dict(ckpt["model_state_dict"])
    target_net.load_state_dict(ckpt["model_state_dict"])
    if optimizer is not None and "optimizer_state_dict" in ckpt:
        try:
            optimizer.load_state_dict(ckpt["optimizer_state_dict"])
        except Exception as e:
            print(f"  (optimizer state didn't load cleanly, continuing with fresh optimizer: {e})")

    episode = ckpt.get("episode", 0)
    print(f"Resumed from '{path}' at episode {episode}")
    return episode


class Shared:
    """Everything worker threads and the trainer thread touch concurrently."""

    def __init__(self, device, buffer_size, lr, checkpoint_path, resume):
        self.device = device
        self.q_net = QNetwork(STATE_DIM, N_ACTIONS).to(device)
        self.target_net = QNetwork(STATE_DIM, N_ACTIONS).to(device)
        self.optimizer = torch.optim.Adam(self.q_net.parameters(), lr=lr)

        self.episode_count = 0
        if resume:
            self.episode_count = load_checkpoint_into(
                checkpoint_path, self.q_net, self.target_net, self.optimizer, device)
        self.target_net.load_state_dict(self.q_net.state_dict())
        self.target_net.eval()

        self.buffer = ReplayBuffer(buffer_size, STATE_DIM)  # thread-safe internally

        self.lock = threading.Lock()   # guards episode_count / scores / net-sync
        self.scores = []
        self.train_steps = 0


async def worker_loop(worker_id, env, shared, args, stop_event):
    while not stop_event.is_set() and (args.max_episodes == 0 or shared.episode_count < args.max_episodes):
        with shared.lock:
            episode = shared.episode_count
        epsilon = compute_epsilon(episode, args)

        state = await env.reset()
        done = False
        info = {"score": 0}

        while not done and not stop_event.is_set():
            # forward-pass inference; brief and fine to call directly from
            # the event loop (the trainer thread updates weights
            # concurrently -- see the staleness note in the README)
            action = select_action(shared.q_net, state, epsilon, N_ACTIONS, shared.device)
            next_state, reward, done, info = await env.step(action)
            shared.buffer.push(state, action, reward, next_state, done)
            state = next_state

        with shared.lock:
            shared.episode_count += 1
            ep_num = shared.episode_count
            shared.scores.append(info["score"])
            avg20 = float(np.mean(shared.scores[-20:]))
            do_checkpoint = (ep_num % args.checkpoint_every == 0)

        print(f"[worker {worker_id}] ep {ep_num:5d}  score {info['score']:4d}  "
              f"avg20 {avg20:6.1f}  eps {epsilon:.3f}  buffer {len(shared.buffer):6d}")

        if do_checkpoint:
            save_checkpoint(args.checkpoint, shared.q_net, shared.optimizer, ep_num)
            print(f"  saved checkpoint -> {args.checkpoint}")


def trainer_loop(shared, args, stop_flag):
    """Runs on its own OS thread. Never touches Playwright objects, so
    there's no conflict with the asyncio event loop driving the envs."""
    while not stop_flag.is_set():
        if len(shared.buffer) < args.batch_size:
            time.sleep(0.05)
            continue
        train_step(shared.q_net, shared.target_net, shared.optimizer,
                   shared.buffer, args.batch_size, args.gamma, shared.device)
        shared.train_steps += 1
        if shared.train_steps % args.target_sync_every == 0:
            with shared.lock:
                shared.target_net.load_state_dict(shared.q_net.state_dict())


async def _run_envs(args, shared):
    stop_event = asyncio.Event()
    async with async_playwright() as pw:
        browser = await pw.chromium.launch(headless=True)
        envs = [
            AsyncDinoEnv(url=args.url, browser=browser, step_delay=args.step_delay,
                         player_name=f"Arthur Isaac")
            for i in range(args.num_envs)
        ]
        tasks = [
            asyncio.create_task(worker_loop(i, envs[i], shared, args, stop_event))
            for i in range(args.num_envs)
        ]
        try:
            await asyncio.gather(*tasks)
        finally:
            for env in envs:
                await env.close()
            await browser.close()


def run_parallel(args):
    device = get_device()
    print(f"Using device: {device}  |  {args.num_envs} parallel envs (asyncio)")

    shared = Shared(device, args.buffer_size, args.lr, args.checkpoint, resume=not args.fresh)
    stop_flag = threading.Event()
    trainer_thread = threading.Thread(target=trainer_loop, args=(shared, args, stop_flag), daemon=True)
    trainer_thread.start()

    try:
        asyncio.run(_run_envs(args, shared))
    except KeyboardInterrupt:
        print("\nInterrupted -- stopping workers and saving checkpoint")
    finally:
        stop_flag.set()
        trainer_thread.join(timeout=5)
        save_checkpoint(args.checkpoint, shared.q_net, shared.optimizer, shared.episode_count)
        print(f"Final checkpoint saved to {args.checkpoint} after {shared.episode_count} episodes")


def run_show(args):

    device = get_device()
    print(f"Using device: {device}  |  single visible env with live state plot")

    q_net = QNetwork(STATE_DIM, N_ACTIONS).to(device)
    target_net = QNetwork(STATE_DIM, N_ACTIONS).to(device)
    optimizer = torch.optim.Adam(q_net.parameters(), lr=args.lr)

    start_episode = 0
    if not args.fresh:
        start_episode = load_checkpoint_into(args.checkpoint, q_net, target_net, optimizer, device)
    target_net.load_state_dict(q_net.state_dict())
    target_net.eval()
    buffer = ReplayBuffer(args.buffer_size, STATE_DIM)

    # disable_score_submit=False: --show plays through window.__rl exactly
    # like a real visitor would, so scores post to /api/scores/submit as
    # if this were a genuine deployment play session -- even though the
    # network is still training in the background on every step below.
    env = DinoEnv(url=args.url, headless=False, step_delay=args.step_delay,
                  disable_score_submit=False)

    # One heatmap per nn.Linear layer in the network (weight matrix:
    # rows = output units, cols = input units). Discovered dynamically so
    # this keeps working if you change QNetwork's architecture later.
    linear_layers = [m for m in q_net.net if isinstance(m, torch.nn.Linear)]



    episode = start_episode
    global_step = 0
    scores = []

    try:
        while args.max_episodes == 0 or episode < args.max_episodes:
            state = env.reset()
            done = False
            epsilon = compute_epsilon(episode, args)

            while not done:
                action = select_action(q_net, state, epsilon, N_ACTIONS, device)
                next_state, reward, done, info = env.step(action)
                buffer.push(state, action, reward, next_state, done)
                state = next_state
                global_step += 1

                if len(buffer) >= args.batch_size:
                    train_step(q_net, target_net, optimizer, buffer, args.batch_size, args.gamma, device)
                    if global_step % args.target_sync_every == 0:
                        target_net.load_state_dict(q_net.state_dict())

                # live update of the network's weight matrices -- only
                # meaningfully changes after a train_step, but redrawing
                # every env step is cheap enough here


            episode += 1
            scores.append(info["score"])
            print(f"ep {episode:4d}  score {info['score']:4d}  avg20 {np.mean(scores[-20:]):6.1f}  eps {epsilon:.3f}")

            if episode % args.checkpoint_every == 0:
                save_checkpoint(args.checkpoint, q_net, optimizer, episode)
                print(f"  saved checkpoint -> {args.checkpoint}")

    except KeyboardInterrupt:
        print("\nInterrupted -- saving final checkpoint")
    finally:
        save_checkpoint(args.checkpoint, q_net, optimizer, episode)
        env.close()
        print(f"Final checkpoint saved to {args.checkpoint} after {episode} episodes")


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--url", required=True, help="e.g. http://localhost:3000")
    ap.add_argument("--show", action="store_true",
                     help="single headed env + live state-array plot (forces --num-envs 1)")
    ap.add_argument("--num-envs", type=int, default=4, help="parallel browser tabs (ignored with --show)")
    ap.add_argument("--step-delay", type=float, default=0.05)
    ap.add_argument("--max-episodes", type=int, default=0, help="0 = run forever (total across all envs)")
    ap.add_argument("--batch-size", type=int, default=64)
    ap.add_argument("--gamma", type=float, default=0.99)
    ap.add_argument("--lr", type=float, default=1e-3)
    ap.add_argument("--buffer-size", type=int, default=50_000)
    ap.add_argument("--eps-start", type=float, default=1.0)
    ap.add_argument("--eps-end", type=float, default=0.05)
    ap.add_argument("--eps-decay-episodes", type=int, default=800)
    ap.add_argument("--target-sync-every", type=int, default=500,
                     help="training steps between target-network syncs")
    ap.add_argument("--checkpoint", default="checkpoint.pt")
    ap.add_argument("--checkpoint-every", type=int, default=50, help="episodes")
    ap.add_argument("--fresh", action="store_true",
                     help="ignore any existing checkpoint at --checkpoint and start from scratch")
    args = ap.parse_args()

    if args.show:
        args.num_envs = 1
        run_show(args)
    else:
        run_parallel(args)


if __name__ == "__main__":
    main()
