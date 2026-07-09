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
)

// Register wires all routes onto mux and ensures required directories exist.
func Register(mux *http.ServeMux) {
	if err := os.MkdirAll(filepath.Dir(scoresFile), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}
	mux.HandleFunc("/tv", serveTemplate("tv.html"))
	mux.HandleFunc("/game", handleGame)
	mux.HandleFunc("/api/player", handlePlayer)
	mux.HandleFunc("/api/scores", handleScores)
	mux.HandleFunc("/api/scores/submit", handleSubmit)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))
}

func serveTemplate(name string) http.HandlerFunc {
	path := filepath.Join(templatesDir, name)
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}
