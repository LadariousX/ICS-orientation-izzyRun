(() => {
    "use strict";

    // ---------- Tunables ----------
    const GRAVITY = 9.8*1.6;                // m/s^2
    const JUMP_VELOCITY = 5.2;          // m/s at jump start
    const FAST_FALL_ACCEL = 22;         // extra m/s^2 when ⬇ held mid-air
    const IZZY_METERS_TALL = 1;
    const START_SPEED = 7.5;            // m/s
    const MAX_SPEED = 24.0;             // m/s cap
    const SPEED_RAMP = 0.05;             // m/s added per second of play
    const SPEED_RAMP_ACCEL = 0.008;      // extra m/s^2 — makes ramp itself accelerate
    const SPAWN_MIN_S = 0.65;
    const SPAWN_MAX_S = 1.5;
    const FIRST_SPAWN_S = 0.4;   // gap between end-of-beach and the first obstacle

    // Dive "negative gravity" — pull Izzy down by this many meters while diving,
    // and how quickly to slide toward/away from that offset.
    const DIVE_DEPTH_M = 0.55;
    const DIVE_LERP_RATE = 18;          // per second

    // Which dive frame to hold on while ⬇ is held. Once released, playback
    // continues through the remaining frames before returning to run/jump.
    const DIVE_HOLD_FRAME = 4;

    // Run animation speed scaling. Frame ms in run/frames.json is treated as
    // the value at START_SPEED; effective ms scales inversely with actual speed.
    const RUN_MS_FLOOR = 30;            // never faster than this per frame

    // Static sprite srcs (GIFs used as fallback if frames.json is missing).
    const SPRITE = {
        run:  "/assets/sprites/run.gif",
        jump: "/assets/sprites/jump.gif",
        dive: "/assets/sprites/dive.gif",
        wave: "/assets/sprites/wave.png",
        pelican: "/assets/sprites/pelican.png",
    };

    // Per-frame timing manifests loaded at boot from /assets/sprites/frames/<kind>/frames.json.
    // Structure: { width, height, frames: [{file, ms}] }
    const frameSets = { run: null, dive: null };

    async function loadFrames(kind) {
        try {
            const res = await fetch(`/assets/sprites/frames/${kind}/frames.json`, { cache: "no-store" });
            if (!res.ok) return;
            const data = await res.json();
            frameSets[kind] = data;
            for (const f of data.frames) {
                const img = new Image();
                img.src = `/assets/sprites/frames/${kind}/${f.file}`;
            }
        } catch (_) { /* leave null → falls back to gif */ }
    }

    // Sky scrolls at this fraction of world speed for a parallax feel.
    const SKY_PARALLAX = 0.25;

    // Death sprite (shown in-place of Izzy after collision).
    const GAME_OVER_SRC = "/assets/sprites/game over.gif";

    // Landing screen scroll speeds (px/s of vertical background-position movement,
    // negative = image content scrolls UPWARD in the viewport).
    const LANDING_SCROLL_LOOP = 90;
    const LANDING_REVEAL_MS = 700;   // matches the CSS transition on .landing

    // ---------- DOM ----------
    const skyEl        = document.getElementById("sky");
    const groundEl     = document.getElementById("ground");
    const beachEl      = document.getElementById("beach");
    const izzyEl       = document.getElementById("izzy");
    const obstaclesEl  = document.getElementById("obstacles");
    const scoreEl      = document.getElementById("score");
    const goOverlay    = document.getElementById("overlay-gameover");
    const finalScoreEl = document.getElementById("final-score");
    const rankMsgEl    = document.getElementById("rank-msg");
    const btnStart     = document.getElementById("btn-start");
    const btnRestart   = document.getElementById("btn-restart");
    const btnUp        = document.getElementById("btn-up");
    const btnDown      = document.getElementById("btn-down");
    const landingEl    = document.getElementById("landing");
    const landingBgEl  = document.getElementById("landing-bg");
    const landingStripEl = document.getElementById("landing-bg-strip");
    const landingPopup = document.getElementById("landing-popup");
    const nameInput    = document.getElementById("name-input");
    const nameErrorEl  = document.getElementById("name-error");

    // ---------- Game state ----------
    const state = {
        running: false,
        dead: false,
        t: 0,
        speed: START_SPEED,
        bgOffset: 0,
        izzy: {
            y: 0,                    // meters above ground
            vy: 0,                   // m/s
            grounded: true,
            diveOffset: 0,           // meters below neutral (positive = downward pull)
        },
        sprite: {
            kind: "run",             // "run" | "jump" | "dive"
            frameIdx: 0,             // used only for dive
            elapsedMs: 0,            // used only for dive
        },
        input: { up: false, down: false },
        obstacles: [],
        nextSpawnIn: FIRST_SPAWN_S,
        onBeach: true,          // suppress obstacle spawns while beach is still in view
        score: 0,
        distance: 0,
        pxPerMeter: 100,
    };

    function computePxPerMeter() {
        const izzyH = izzyEl.getBoundingClientRect().height || (window.innerHeight * 0.22);
        state.pxPerMeter = izzyH / IZZY_METERS_TALL;
    }

    // ---------- Sprite kind + frame stepping ----------
    function setSpriteKind(kind) {
        if (state.sprite.kind === kind) return;
        state.sprite.kind = kind;
        state.sprite.frameIdx = 0;
        state.sprite.elapsedMs = 0;
        applySpriteFrame();
    }

    function applySpriteFrame() {
        const k = state.sprite.kind;
        const set = frameSets[k];
        if (set && set.frames.length) {
            const f = set.frames[state.sprite.frameIdx];
            izzyEl.src = `/assets/sprites/frames/${k}/${f.file}`;
            return;
        }
        // No manifest → fall back to gif with cache-buster to restart the animation.
        izzyEl.src = SPRITE[k] + (k === "run" ? "" : "?t=" + Date.now());
    }

    // Advance the current sprite up to (and holding at) capIdx. Returns true once
    // playback has fully consumed the frame at capIdx (used to detect end-of-outro).
    // frameMsScale scales the per-frame ms — 1 = as-authored, <1 = faster, >1 = slower.
    function advanceSprite(dt, capIdx, frameMsScale) {
        const s = state.sprite;
        const set = frameSets[s.kind];
        if (!set || !set.frames.length) return true;

        const cap = Math.min(capIdx, set.frames.length - 1);
        s.elapsedMs += dt * 1000;

        while (s.frameIdx < cap) {
            const frameMs = Math.max(1, set.frames[s.frameIdx].ms * frameMsScale);
            if (s.elapsedMs < frameMs) break;
            s.elapsedMs -= frameMs;
            s.frameIdx++;
            applySpriteFrame();
        }

        // Once at the cap, clamp elapsed to that frame's ms so we don't accumulate
        // forever (which would delay any later state change).
        const capMs = Math.max(1, set.frames[cap].ms * frameMsScale);
        if (s.frameIdx >= cap && s.elapsedMs > capMs) s.elapsedMs = capMs;

        return s.frameIdx >= cap && s.elapsedMs >= capMs;
    }

    // Loop the run animation forever, scaling frame time by current game speed.
    function tickRun(dt) {
        const set = frameSets.run;
        if (!set || !set.frames.length) return;
        const s = state.sprite;
        const scale = START_SPEED / Math.max(0.01, state.speed);
        s.elapsedMs += dt * 1000;
        // Walk the queue in case dt eats multiple frames.
        while (true) {
            const raw = set.frames[s.frameIdx].ms * scale;
            const frameMs = Math.max(RUN_MS_FLOOR, raw);
            if (s.elapsedMs < frameMs) break;
            s.elapsedMs -= frameMs;
            s.frameIdx = (s.frameIdx + 1) % set.frames.length;
            applySpriteFrame();
        }
    }

    // ---------- Input ----------
    function bindHold(el, key) {
        const on = e => {
            e.preventDefault();
            if (el.setPointerCapture) el.setPointerCapture(e.pointerId);
            state.input[key] = true;
        };
        const off = e => {
            e.preventDefault();
            state.input[key] = false;
        };
        el.addEventListener("pointerdown", on);
        el.addEventListener("pointerup",   off);
        el.addEventListener("pointercancel", off);
    }
    bindHold(btnUp,   "up");
    bindHold(btnDown, "down");

    // Don't hijack keys while the player is typing (e.g. spaces in their name),
    // otherwise the jump binding on Space swallows the keystroke.
    const isTyping = e => {
        const el = e.target;
        return el && (el.tagName === "INPUT" || el.tagName === "TEXTAREA" || el.isContentEditable);
    };

    window.addEventListener("keydown", e => {
        if (e.repeat || isTyping(e)) return;
        if (e.key === "ArrowUp" || e.key === " ") { state.input.up = true;   e.preventDefault(); }
        if (e.key === "ArrowDown")                { state.input.down = true; e.preventDefault(); }
    });
    window.addEventListener("keyup", e => {
        if (isTyping(e)) return;
        if (e.key === "ArrowUp" || e.key === " ") state.input.up = false;
        if (e.key === "ArrowDown")                state.input.down = false;
    });

    btnStart.addEventListener("click", beginFromLanding);
    btnRestart.addEventListener("click", startRun);
    window.addEventListener("resize", computePxPerMeter);
    // Enter key in the name input triggers start.
    nameInput.addEventListener("keydown", e => {
        if (e.key === "Enter") { e.preventDefault(); beginFromLanding(); }
    });
    nameInput.addEventListener("input", clearNameError);

    // Handoff from landing → game: capture name, shrink popup, slide landing up,
    // then kick off the actual game.
    let landingDone = false;
    let landingBusy = false;
    async function beginFromLanding() {
        if (landingDone || landingBusy) return;

        const raw = (nameInput.value || "").trim().replace(/,/g, "").slice(0, 20);
        if (!raw) {
            showNameError("Enter a name to start");
            return;
        }

        // Register the player before letting them in. This call also gates the
        // name server-side (profanity filter) and captures their location for
        // the TV panel. A 400 means the name was rejected — block and prompt
        // for another. Network errors don't block play at the table.
        landingBusy = true;
        try {
            const res = await fetch("/api/player", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name: raw }),
            });
            if (res.status === 400) {
                landingBusy = false;
                showNameError("Please choose a different name");
                return;
            }
        } catch (_) { /* offline — allow play */ }
        landingBusy = false;

        clearNameError();
        landingDone = true;
        landingScrolling = false;

        sessionStorage.setItem("playerName", raw);

        landingPopup.classList.add("shrink");
        // Small delay so the shrink is visible before the reveal takes off.
        setTimeout(() => landingEl.classList.add("revealing"), 180);
        setTimeout(() => {
            landingEl.classList.add("hidden");
            startRun();
        }, LANDING_REVEAL_MS + 180);
    }

    function showNameError(msg) {
        nameInput.classList.add("input-error");
        if (nameErrorEl) { nameErrorEl.textContent = msg; nameErrorEl.classList.add("show"); }
        nameInput.focus();
    }
    function clearNameError() {
        nameInput.classList.remove("input-error");
        if (nameErrorEl) { nameErrorEl.classList.remove("show"); nameErrorEl.textContent = ""; }
    }

    function startRun() {
        goOverlay.classList.add("hidden");
        reset();
        state.running = true;
        lastT = performance.now();
        requestAnimationFrame(tick);
    }

    // ---------- Landing background scroll ----------
    // Loops the landing.png vertically (upward) until the user hits Start.
    // Driven by transform: translateY on the strip (not background-position) so
    // Safari re-samples the popup's backdrop-filter as it moves — see game.css.
    let landingScrolling = true;
    let landingY = 0;
    let landingTile = 0;        // px height of one rendered image tile (wrap period)
    let landingImgAspect = 0;   // naturalHeight / naturalWidth
    { const img = new Image();
      img.onload = () => {
          if (img.naturalWidth) landingImgAspect = img.naturalHeight / img.naturalWidth;
          sizeLandingStrip();
      };
      img.src = "/assets/sprites/landing.png"; }

    // With background-size: 100% auto the tile scales to the strip width, so the
    // vertical repeat period is width * aspect. Size the strip to the viewport
    // plus one tile so scrolling up never exposes a gap below it.
    function sizeLandingStrip() {
        if (!landingImgAspect) return;
        landingTile = window.innerWidth * landingImgAspect;
        landingStripEl.style.height = Math.ceil(window.innerHeight + landingTile + 2) + "px";
    }
    window.addEventListener("resize", sizeLandingStrip);

    let lastLandingT = performance.now();
    function landingTick(now) {
        const dt = Math.min(0.05, (now - lastLandingT) / 1000);
        lastLandingT = now;
        if (landingScrolling) {
            landingY -= LANDING_SCROLL_LOOP * dt;
            // Wrap by exactly one tile — repeat-y makes that visually seamless.
            if (landingTile > 0 && landingY <= -landingTile) landingY += landingTile;
            landingStripEl.style.transform = `translateY(${landingY}px)`;
            requestAnimationFrame(landingTick);
        }
    }
    requestAnimationFrame(landingTick);

    // ---------- Core loop ----------
    let lastT = 0;

    function reset() {
        state.dead = false;
        state.t = 0;
        state.speed = START_SPEED;
        state.bgOffset = 0;
        state.izzy.y = 0;
        state.izzy.vy = 0;
        state.izzy.grounded = true;
        state.izzy.diveOffset = 0;
        state.sprite.kind = "run";
        state.sprite.frameIdx = 0;
        state.sprite.elapsedMs = 0;
        izzyEl.style.transform = "";
        applySpriteFrame();
        state.obstacles.forEach(o => o.el.remove());
        state.obstacles.length = 0;
        state.nextSpawnIn = FIRST_SPAWN_S;
        state.onBeach = true;
        beachEl.style.transform = "translateX(0)";
        state.score = 0;
        state.distance = 0;
        scoreEl.textContent = "0";
        computePxPerMeter();
    }

    function tick(now) {
        const dt = Math.min(0.05, (now - lastT) / 1000);
        lastT = now;
        if (state.running && !state.dead) update(dt);
        render();
        if (state.running) requestAnimationFrame(tick);
    }

    function update(dt) {
        state.t += dt;
        // Speed grows linearly, then accelerates over time (t^2 term) so
        // late-game feels distinctly faster than mid-game.
        state.speed = Math.min(
            MAX_SPEED,
            START_SPEED + SPEED_RAMP * state.t + 0.5 * SPEED_RAMP_ACCEL * state.t * state.t
        );
        state.distance += state.speed * dt;
        state.score = Math.floor(state.distance * 4);

        const z = state.izzy;

        // Jump edge trigger.
        if (state.input.up && z.grounded) {
            z.vy = JUMP_VELOCITY;
            z.grounded = false;
        }

        // Gravity + optional fast-fall while airborne.
        const gravity = GRAVITY + (state.input.down && !z.grounded ? FAST_FALL_ACCEL : 0);
        z.vy -= gravity * dt;
        z.y  += z.vy * dt;
        if (z.y <= 0) { z.y = 0; z.vy = 0; z.grounded = true; }

        // What state does input+physics want us in this frame?
        let want;
        if (!z.grounded)              want = (z.vy < 0 && state.input.down) ? "dive" : "jump";
        else if (state.input.down)    want = "dive";
        else                          want = "run";

        // Dive playback: hold on DIVE_HOLD_FRAME while ⬇ is held, and play out
        // the remaining frames once released before switching to run/jump.
        if (state.sprite.kind === "dive") {
            const diveSet = frameSets.dive;
            const lastIdx = diveSet ? diveSet.frames.length - 1 : DIVE_HOLD_FRAME;
            if (want === "dive") {
                // If input is bouncing back on during outro, snap back to hold.
                if (state.sprite.frameIdx > DIVE_HOLD_FRAME) {
                    state.sprite.frameIdx = DIVE_HOLD_FRAME;
                    state.sprite.elapsedMs = 0;
                    applySpriteFrame();
                }
                advanceSprite(dt, DIVE_HOLD_FRAME, 1);
            } else {
                const done = advanceSprite(dt, lastIdx, 1);
                if (done) setSpriteKind(want);
            }
        } else {
            if (want !== state.sprite.kind) setSpriteKind(want);
            if (want === "run") tickRun(dt);
            // "jump" is a gif or a single held sprite — no manual stepping.
        }

        // "Negative gravity" — pull Izzy down on screen by a set distance while diving.
        const targetOffset = (want === "dive") ? DIVE_DEPTH_M : 0;
        const lerp = Math.min(1, DIVE_LERP_RATE * dt);
        z.diveOffset += (targetOffset - z.diveOffset) * lerp;

        // Beach: track whether it's still on-screen. Suppress spawns until it's fully past.
        if (state.onBeach) {
            const beachW = beachEl.getBoundingClientRect().width || 0;
            // Beach's right edge in viewport = beachW + bgOffset (since translateX(bgOffset)).
            if (beachW > 0 && beachW + state.bgOffset < 0) {
                state.onBeach = false;
            }
        }

        // Obstacles — pause spawning while the beach is still visible.
        if (!state.onBeach) {
            state.nextSpawnIn -= dt;
            if (state.nextSpawnIn <= 0) {
                spawnObstacle();
                const min = Math.max(0.70, SPAWN_MIN_S - state.t * 0.01);
                const max = Math.max(min + 0.2, SPAWN_MAX_S - state.t * 0.02);
                state.nextSpawnIn = min + Math.random() * (max - min);
            }
        }
        for (let i = state.obstacles.length - 1; i >= 0; i--) {
            const o = state.obstacles[i];
            o.xPx -= state.speed * state.pxPerMeter * dt;
            if (o.xPx + o.wPx < -40) {
                o.el.remove();
                state.obstacles.splice(i, 1);
                continue;
            }
            if (checkCollision(o)) { die(o); return; }
        }

        state.bgOffset -= state.speed * state.pxPerMeter * dt;
    }

    function render() {
        const z = state.izzy;
        // Skip Izzy transform while dead — die() has already parked her in the pose.
        if (!state.dead) {
            const yPx = (-z.y + z.diveOffset) * state.pxPerMeter;
            izzyEl.style.transform = `translateY(${yPx}px)`;
        }
        scoreEl.textContent = state.score.toString();
        skyEl.style.backgroundPosition = `${state.bgOffset * SKY_PARALLAX}px 0`;
        groundEl.style.backgroundPosition = `${state.bgOffset}px 0`;
        beachEl.style.transform = `translateX(${state.bgOffset}px)`;
        for (const o of state.obstacles) {
            o.el.style.transform = `translateX(${o.xPx}px)`;
        }
    }

    // ---------- Obstacles ----------
    function spawnObstacle() {
        const isPelican = Math.random() < 0.35;
        const el = document.createElement("img");
        el.className = "obstacle " + (isPelican ? "obstacle--pelican" : "obstacle--wave");
        el.src = isPelican ? SPRITE.pelican : SPRITE.wave;
        el.alt = "";
        obstaclesEl.appendChild(el);

        const startX = window.innerWidth + 40;
        const o = { kind: isPelican ? "pelican" : "wave", el, xPx: startX, wPx: 100, hPx: 100 };
        el.style.transform = `translateX(${startX}px)`;
        el.onload = () => {
            const r = el.getBoundingClientRect();
            o.wPx = r.width;
            o.hPx = r.height;
        };
        state.obstacles.push(o);
    }

    // ---------- Collision (AABB with forgiveness) ----------
    function checkCollision(o) {
        const izzyR = izzyEl.getBoundingClientRect();
        const oR    = o.el.getBoundingClientRect();
        const shrink = 0.15;
        const ix1 = izzyR.left  + izzyR.width  * shrink;
        const ix2 = izzyR.right - izzyR.width  * shrink;
        const iy1 = izzyR.top   + izzyR.height * shrink;
        const iy2 = izzyR.bottom- izzyR.height * shrink;
        const ox1 = oR.left  + oR.width  * shrink;
        const ox2 = oR.right - oR.width  * shrink;
        const oy1 = oR.top   + oR.height * shrink;
        const oy2 = oR.bottom- oR.height * shrink;
        return ix1 < ox2 && ix2 > ox1 && iy1 < oy2 && iy2 > oy1;
    }

    // ---------- Death / submit ----------
    async function die(killer) {
        // Grab rects BEFORE swapping src (game-over gif may not be laid out yet).
        const izzyR = izzyEl.getBoundingClientRect();
        const killerR = killer ? killer.el.getBoundingClientRect() : null;

        state.dead = true;

        // Swap in the game-over sprite in place of Izzy.
        izzyEl.src = GAME_OVER_SRC;

        // Position: on the ground (translateY = 0). If a wave killed her,
        // tuck her just behind (to the left of) it so the wave visually
        // rolled over Izzy.
        let deathX = 0;
        if (killer && killer.kind === "wave" && killerR) {
            deathX = killerR.left - izzyR.right;
        }
        izzyEl.style.transform = `translate(${deathX}px, 0)`;

        state.running = false;
        finalScoreEl.textContent = state.score.toString();
        rankMsgEl.textContent = "";
        goOverlay.classList.remove("hidden");

        try {
            const res = await fetch("/api/scores/submit", {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({ name: getPlayerName(), score: state.score }),
            });
            if (res.ok) {
                const data = await res.json();
                // Only celebrate when this run beat the player's own best.
                if (data.newHigh) {
                    if (data.record)                        rankMsgEl.textContent = "New high score — #1 on the leaderboard!";
                    else if (data.rank && data.rank <= 5)   rankMsgEl.textContent = `New high score — #${data.rank} on the leaderboard!`;
                    else                                    rankMsgEl.textContent = "New high score!";
                }
            }
        } catch (_) { /* offline — ignore */ }
    }

    function getPlayerName() {
        return sessionStorage.getItem("playerName") || "Player";
    }

    // ---------- Boot ----------
    loadFrames("dive");
    loadFrames("run");
    // Preload the game-over sprite so the death swap is instant.
    { const img = new Image(); img.src = GAME_OVER_SRC; }
    computePxPerMeter();
    izzyEl.addEventListener("load", computePxPerMeter, { once: true });
})();
