package app

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	templatesDir = "templates"
	staticDir    = "static"
	assetsDir    = "assets"

	// localPort is the address used for local dev and RPi hosting. Any other
	// listen port is treated as a secondary/dev origin whose page routes
	// redirect to the canonical public site.
	localPort = ":8080"
	// canonicalBase is the public URL that fronts the RPi instance.
	canonicalBase = "https://laydenb.com/ics"
)

// Serve builds a mux, registers all routes under the given page prefix, and
// starts an HTTP server on port. prefix affects only the page routes (/tv and
// /game); the API and static/asset paths stay at the root of the origin, which
// is why the frontend's absolute URLs keep working unchanged.
//
// When port is not localPort (:8080), the page routes redirect to canonicalBase
// (/tv -> laydenb.com/ics/tv, /game -> laydenb.com/ics/game) so users always
// land on the canonical site instead of a secondary origin.
func Serve(port, prefix string) error {
	mux := http.NewServeMux()
	canonical := ""
	if port != localPort {
		canonical = canonicalBase
	}
	Register(mux, prefix, canonical)
	log.Printf("listening on %s", port)
	if canonical != "" {
		log.Printf("page routes redirect to %s", canonical)
	} else {
		log.Printf("Dashboard at http://localhost%s%s/tv", port, prefix)
	}
	return http.ListenAndServe(port, mux)
}

// Register wires all routes onto mux and ensures required directories exist.
// Page routes (/tv, /game) are served under prefix ("" for the default server,
// e.g. "/izzygame" for a second instance); API and static/asset routes stay at
// the root so the frontend's absolute paths resolve on the same origin.
//
// When canonical is non-empty, the page routes issue redirects to
// canonical+"/tv" and canonical+"/game" instead of serving the pages locally.
func Register(mux *http.ServeMux, prefix, canonical string) {
	if err := os.MkdirAll(filepath.Dir(scoresFile), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	if canonical != "" {
		mux.HandleFunc(prefix+"/tv", redirectTo(canonical+"/tv"))
		mux.HandleFunc(prefix+"/game", redirectTo(canonical+"/game"))
	} else {
		mux.HandleFunc(prefix+"/tv", serveTemplate("tv.html"))
		mux.HandleFunc(prefix+"/game", handleGame)
	}
	mux.HandleFunc("/api/player", handlePlayer)
	mux.HandleFunc("/api/scores", handleScores)
	mux.HandleFunc("/api/scores/submit", handleSubmit)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))
}

// redirectTo returns a handler that sends every request to target with a 302,
// preserving the client's method-agnostic navigation to the canonical site.
func redirectTo(target string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target, http.StatusFound)
	}
}

func serveTemplate(name string) http.HandlerFunc {
	path := filepath.Join(templatesDir, name)
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}
