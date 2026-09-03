package app

import (
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	templatesDir = "templates"
	staticDir    = "static"
	assetsDir    = "assets"

	// slideshowFile is served by its own route rather than through the /assets/
	// file server — see the slideshowRoute registration in Register.
	slideshowFile  = assetsDir + "/slideshow/slideshowCompressed.mp4"
	slideshowRoute = "/assets/slideshow/video"
)

// basePath is the URL prefix the game is mounted under, read from BASE_PATH and
// defaulting to "/". In production the game lives at a subpath of another site
// (BASE_PATH=/ics/izzy-game/) and the reverse proxy strips that prefix before
// forwarding, so routes are still registered at this app's own root either way.
// basePath exists purely to build URLs that are handed back to the browser.
var basePath = normalizeBasePath(os.Getenv("BASE_PATH"))

// pageData is the render context for every HTML template.
type pageData struct {
	BasePath string
}

// templates holds all of templates/*.html, parsed once at startup so a broken
// template fails the boot instead of the first request.
var templates *template.Template

// normalizeBasePath forces a leading and a trailing slash. The trailing slash is
// load-bearing: <base href="/ics/izzy-game"> without it resolves
// "static/css/game.css" to "/ics/static/css/game.css".
func normalizeBasePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// Serve opens the score database, parses the templates, builds a mux with all
// routes, and starts an HTTP server on addr.
func Serve(addr string) error {
	if err := openDB(); err != nil {
		return err
	}
	if err := loadTemplates(); err != nil {
		return err
	}
	mux := http.NewServeMux()
	Register(mux)
	log.Printf("listening on %s - base path %s - game at %s, dashboard at %stv",
		addr, basePath, basePath, basePath)
	return http.ListenAndServe(addr, mux)
}

// loadTemplates parses templates/*.html into the package-level set. Safe to call
// more than once; the last call wins.
func loadTemplates() error {
	t, err := template.ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		return err
	}
	templates = t
	return nil
}

// Register wires all routes onto mux. The game is the app's root; "/{$}" matches
// only the exact path, so unknown paths still 404 instead of being swallowed by
// a catch-all. /game redirects to the root because the printed QR code in
// assets/img/gameQR.png points at the old path.
//
// Paths here are the app's own, not the browser's — see basePath.
//
// Callers using Register directly must call openDB and loadTemplates first;
// Serve does both for you.
func Register(mux *http.ServeMux) {
	mux.HandleFunc("/{$}", serveTemplate("game.html"))
	mux.HandleFunc("/game", redirectTo(basePath))
	mux.HandleFunc("/tv", serveTemplate("tv.html"))
	mux.HandleFunc("/api/player", handlePlayer)
	mux.HandleFunc("/api/scores", handleScores)
	mux.HandleFunc("/api/scores/submit", handleSubmit)
	// The dashboard video gets an extension-less URL on purpose. nginx in the
	// Site repo has a `location ~* \.(mp4|mov|webm)$` block ahead of the
	// `/ics/izzy-game/` prefix that routes here, and regex locations win over
	// prefix ones — so any URL ending in .mp4 is handed to the blog, which 404s
	// it, and the dashboard plays nothing. A path that regex cannot match keeps
	// the video working whether or not the Site repo ever adds `^~`. This
	// pattern is longer than "/assets/", so the mux prefers it.
	mux.HandleFunc(slideshowRoute, serveSlideshow)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))
}

// serveSlideshow sends the dashboard video. http.ServeFile rather than a plain
// copy so byte-range requests keep working — browsers probe a video with one
// before they will play it — and so Content-Type is still derived from the
// file's own .mp4 extension even though the URL no longer carries it.
func serveSlideshow(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, slideshowFile)
}

// redirectTo returns a handler that sends every request to target with a 302.
// target must already be browser-facing (i.e. include basePath).
func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}
}

// serveTemplate renders one of the parsed templates with the base path injected,
// so the markup can reference assets relatively and still resolve under a
// subpath mount.
func serveTemplate(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := templates.ExecuteTemplate(w, name, pageData{BasePath: basePath}); err != nil {
			// Headers are likely already flushed, so this can't become a clean
			// 500 — log it and let the truncated response speak for itself.
			log.Printf("render %s: %v", name, err)
		}
	}
}
