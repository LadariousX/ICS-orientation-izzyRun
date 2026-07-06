(() => {
    const POLL_MS = 3000;

    const listEl = document.getElementById("leaderboard-list");
    const video = document.getElementById("slideshow-video");
    const mapEl = document.getElementById("ipinfo-map");

    let lastLeaderboardKey = "";
    let lastMapKey = "";
    const textCache = {};

    async function refresh() {
        try {
            const res = await fetch("/api/scores", { cache: "no-store" });
            if (!res.ok) return;
            const data = await res.json();
            renderLeaderboard(data.top || []);
            renderIPInfo(data.ipinfo || null);
        } catch (_) { /* transient */ }
    }

    function renderLeaderboard(top) {
        const key = JSON.stringify(top);
        if (key === lastLeaderboardKey) return;
        lastLeaderboardKey = key;

        if (!top.length) {
            listEl.innerHTML = '<li class="leaderboard__empty">Waiting for the first score...</li>';
            return;
        }
        listEl.innerHTML = top
            .map((s, i) => {
                const rank = i + 1;
                const cls = rank === 1 ? "top" : "";
                return `<li class="${cls}"><span class="rank">#${rank}</span><span class="name">${escapeHtml(s.name)}</span><span class="score">${s.score}</span></li>`;
            })
            .join("");
    }

    function renderIPInfo(info) {
        if (!info || info.status !== "ok") {
            setText("ipinfo-ip",       info && info.ip ? info.ip : "—");
            setText("ipinfo-hostname", "—");
            setText("ipinfo-isp",      "—");
            setText("ipinfo-loc",      info && info.status === "private" ? "Private / local network" : "Waiting for a player...");
            setText("ipinfo-dist",     "—");
            updateMap(null, null);
            return;
        }
        setText("ipinfo-ip",       info.ip || "—");
        setText("ipinfo-hostname", info.hostname || "N/A");
        setText("ipinfo-isp",      info.isp || info.org || "N/A");
        setText("ipinfo-loc",      [info.city, info.region, info.country].filter(Boolean).join(", ") || "—");
        setText("ipinfo-dist",     Number.isFinite(info.distanceMiles) ? `${Math.round(info.distanceMiles)} mi` : "—");
        updateMap(info.lat, info.lon);
    }

    // Only reload the iframe when the rounded coordinates actually change,
    // otherwise the map flickers every poll.
    function updateMap(lat, lon) {
        const key = (lat == null || lon == null) ? "none" : `${lat.toFixed(3)},${lon.toFixed(3)}`;
        if (key === lastMapKey) return;
        lastMapKey = key;
        if (key === "none") {
            if (mapEl.src !== "about:blank") mapEl.src = "about:blank";
            return;
        }
        const d = 0.05;
        const bbox = [lon - d, lat - d, lon + d, lat + d].join(",");
        mapEl.src = `https://www.openstreetmap.org/export/embed.html?bbox=${bbox}&layer=mapnik&marker=${lat},${lon}`;
    }

    function setText(id, val) {
        if (textCache[id] === val) return;
        textCache[id] = val;
        const el = document.getElementById(id);
        if (el) el.textContent = val;
    }

    function escapeHtml(s) {
        return String(s).replace(/[&<>"']/g, c => ({
            "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;"
        })[c]);
    }

    if (video) {
        const tryPlay = () => video.play().catch(() => {});
        tryPlay();
        video.addEventListener("canplay", tryPlay, { once: true });
    }

    refresh();
    setInterval(refresh, POLL_MS);
})();
