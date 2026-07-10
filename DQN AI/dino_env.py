"""
Gym-style environment that drives the Izzy Run game through a real browser
via Playwright, using the window.__rl hook added to game.js.

Works identically against localhost (training) and the deployed URL
(inference) -- only the `url` passed to DinoEnv changes.
"""

import time
import asyncio
import numpy as np
from playwright.sync_api import sync_playwright
from playwright.async_api import Browser as AsyncBrowser

STATE_DIM = 10   # must match window.__rl.getState() in game.rl-patched.js
N_ACTIONS = 3    # 0 = noop, 1 = jump, 2 = duck

# Rough normalization so the network isn't staring at wildly different
# scales (meters vs pixels vs m/s). Tune these once you've watched a
# few runs of state values in practice.
STATE_SCALE = np.array([
    1.0,     # izzy.y (meters, ~0-1)
    5.0,     # izzy.vy (m/s, jump velocity ~5.2)
    1.0,     # grounded flag (0/1)
    1.0,     # ducking flag (0/1)
    800.0,   # dist to obstacle 0 (px)
    100.0,   # width of obstacle 0 (px)
    100.0,   # height of obstacle 0 (px)
    1.0,     # is-pelican flag (0/1)
    800.0,   # dist to obstacle 1 (px)
    24.0,    # game speed (m/s, caps at 24)
], dtype=np.float32)


class DinoEnv:
    def __init__(self, url: str, headless: bool = True, step_delay: float = 0.05,
                 player_name: str = "Arthur Isaac", disable_score_submit: bool = True,
                 slow_mo: int = 0, playwright=None, browser=None):
        """
        By default launches its own Playwright + browser (fine for a single
        env, e.g. play_prod.py). Pass an existing `playwright`/`browser` to
        share one Chromium process across many envs -- each still gets its
        own isolated context/page/cookies, just without the overhead of a
        separate browser process per env. See train.py's parallel mode.
        """
        self._owns_playwright = playwright is None
        self._owns_browser = browser is None
        self._pw = playwright or sync_playwright().start()
        self.browser = browser or self._pw.chromium.launch(headless=headless, slow_mo=slow_mo)
        self.context = self.browser.new_context(viewport={"width": 1000, "height": 600})
        self.page = self.context.new_page()
        self.page.goto(url)
        self.step_delay = step_delay
        self.player_name = player_name
        self._landed = False
        self._disable_score_submit = disable_score_submit
        self._last_score = 0

    def _do_landing_once(self):
        """Fills the name field and clicks Start -- only needed the very
        first time, since restarts after that call __rl.reset() directly."""
        self.page.wait_for_function("() => !!window.__rl", timeout=15000)
        if self._disable_score_submit:
            self.page.evaluate("window.__rl.disableScoreSubmit = true")

        self.page.fill("#name-input", self.player_name)
        self.page.click("#btn-start")
        # landing reveal animation + first startRun() takes ~1s (see
        # LANDING_REVEAL_MS in game.js) -- wait until the game actually
        # reports running rather than guessing a sleep duration.
        self.page.wait_for_function("() => window.__rl.isRunning()", timeout=10000)
        self._landed = True

    def reset(self) -> np.ndarray:
        if not self._landed:
            self._do_landing_once()
        else:
            self.page.evaluate("window.__rl.reset()")
            self.page.wait_for_function("() => window.__rl.isRunning()", timeout=5000)

        self._last_score = 0
        return self._get_state()

    def _get_state(self) -> np.ndarray:
        raw = self.page.evaluate("window.__rl.getState()")
        return np.array(raw, dtype=np.float32) / STATE_SCALE

    def step(self, action: int):
        self.page.evaluate("(a) => window.__rl.applyAction(a)", action)
        time.sleep(self.step_delay)

        done = bool(self.page.evaluate("window.__rl.isDone()"))
        score = int(self.page.evaluate("window.__rl.getScore()"))
        state = self._get_state()

        # Reward: small per-step survival bonus, credit for score deltas,
        # a death penalty. Cheap and effective starting point -- tune once
        # you see training curves.
        reward = 0.1 + 0.1 * max(0, score - self._last_score)
        if done:
            reward = -1.0
        self._last_score = score

        return state, reward, done, {"score": score}

    def close(self):
        self.context.close()
        if self._owns_browser:
            self.browser.close()
        if self._owns_playwright:
            self._pw.stop()


class AsyncDinoEnv:
    """
    Async counterpart to DinoEnv, used only by train.py's parallel mode.

    Playwright's *sync* API (used above) is bound to whichever thread
    created it -- objects can't be driven from a different thread, which is
    why parallel training doesn't use threads full of sync DinoEnv
    instances. This class instead runs on asyncio in a single thread, which
    lets many tabs make real concurrent progress (each awaits network I/O
    to the browser, yielding to the others) while still sharing one
    Chromium process cheaply via separate contexts.
    """

    def __init__(self, url: str, browser: AsyncBrowser, step_delay: float = 0.05,
                 player_name: str = "rl-bot", disable_score_submit: bool = True):
        self.url = url
        self.browser = browser
        self.step_delay = step_delay
        self.player_name = player_name
        self.disable_score_submit = disable_score_submit
        self.context = None
        self.page = None
        self._landed = False
        self._last_score = 0

    async def _ensure_page(self):
        if self.page is None:
            self.context = await self.browser.new_context(viewport={"width": 1000, "height": 600})
            self.page = await self.context.new_page()
            await self.page.goto(self.url)

    async def _do_landing_once(self):
        await self.page.wait_for_function("() => !!window.__rl", timeout=15000)
        if self.disable_score_submit:
            await self.page.evaluate("window.__rl.disableScoreSubmit = true")

        await self.page.fill("#name-input", self.player_name)
        await self.page.click("#btn-start")
        await self.page.wait_for_function("() => window.__rl.isRunning()", timeout=10000)
        self._landed = True

    async def reset(self) -> np.ndarray:
        await self._ensure_page()
        if not self._landed:
            await self._do_landing_once()
        else:
            await self.page.evaluate("window.__rl.reset()")
            await self.page.wait_for_function("() => window.__rl.isRunning()", timeout=5000)

        self._last_score = 0
        return await self._get_state()

    async def _get_state(self) -> np.ndarray:
        raw = await self.page.evaluate("window.__rl.getState()")
        return np.array(raw, dtype=np.float32) / STATE_SCALE

    async def step(self, action: int):
        await self.page.evaluate("(a) => window.__rl.applyAction(a)", action)
        await asyncio.sleep(self.step_delay)

        done = bool(await self.page.evaluate("window.__rl.isDone()"))
        score = int(await self.page.evaluate("window.__rl.getScore()"))
        state = await self._get_state()

        reward = 0.1 + 0.1 * max(0, score - self._last_score)
        if done:
            reward = -1.0
        self._last_score = score

        return state, reward, done, {"score": score}

    async def close(self):
        if self.context is not None:
            await self.context.close()
