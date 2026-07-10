"""
Small DQN: MLP over the state vector, numpy-backed replay buffer, torch
training step. No image input, no CNN -- the state vector is already the
compact representation, which is the whole point of instrumenting the game
instead of reading pixels.
"""

import random
import threading
import numpy as np
import torch
import torch.nn as nn
import torch.nn.functional as F


class QNetwork(nn.Module):
    def __init__(self, state_dim: int, n_actions: int, hidden: int = 128):
        super().__init__()
        self.net = nn.Sequential(
            nn.Linear(state_dim, hidden),
            nn.ReLU(),
            nn.Linear(hidden, hidden),
            nn.ReLU(),
            nn.Linear(hidden, n_actions),
        )

    def forward(self, x):
        return self.net(x)


class ReplayBuffer:
    """Fixed-size ring buffer backed by numpy arrays -- avoids the overhead
    of a Python deque of tuples once you're sampling thousands of times."""

    def __init__(self, capacity: int, state_dim: int):
        self.capacity = capacity
        self.state_dim = state_dim
        self.states = np.zeros((capacity, state_dim), dtype=np.float32)
        self.next_states = np.zeros((capacity, state_dim), dtype=np.float32)
        self.actions = np.zeros(capacity, dtype=np.int64)
        self.rewards = np.zeros(capacity, dtype=np.float32)
        self.dones = np.zeros(capacity, dtype=np.float32)
        self.size = 0
        self.ptr = 0
        self._lock = threading.Lock()

    def push(self, state, action, reward, next_state, done):
        with self._lock:
            i = self.ptr
            self.states[i] = state
            self.next_states[i] = next_state
            self.actions[i] = action
            self.rewards[i] = reward
            self.dones[i] = float(done)
            self.ptr = (self.ptr + 1) % self.capacity
            self.size = min(self.size + 1, self.capacity)

    def sample(self, batch_size: int):
        with self._lock:
            idx = np.random.randint(0, self.size, size=batch_size)
            return (
                self.states[idx].copy(),
                self.actions[idx].copy(),
                self.rewards[idx].copy(),
                self.next_states[idx].copy(),
                self.dones[idx].copy(),
            )

    def __len__(self):
        with self._lock:
            return self.size


def select_action(q_net, state, epsilon, n_actions, device):
    if random.random() < epsilon:
        return random.randrange(n_actions)
    with torch.no_grad():
        s = torch.from_numpy(state).unsqueeze(0).to(device)
        q = q_net(s)
        return int(q.argmax(dim=1).item())


def train_step(q_net, target_net, optimizer, buffer, batch_size, gamma, device):
    if len(buffer) < batch_size:
        return None

    states, actions, rewards, next_states, dones = buffer.sample(batch_size)

    states = torch.from_numpy(states).to(device)
    actions = torch.from_numpy(actions).to(device)
    rewards = torch.from_numpy(rewards).to(device)
    next_states = torch.from_numpy(next_states).to(device)
    dones = torch.from_numpy(dones).to(device)

    q_values = q_net(states).gather(1, actions.unsqueeze(1)).squeeze(1)

    with torch.no_grad():
        next_q = target_net(next_states).max(dim=1)[0]
        target = rewards + gamma * next_q * (1.0 - dones)

    loss = F.smooth_l1_loss(q_values, target)

    optimizer.zero_grad()
    loss.backward()
    torch.nn.utils.clip_grad_norm_(q_net.parameters(), max_norm=10.0)
    optimizer.step()

    return loss.item()
