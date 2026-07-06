package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
)

const (
	addr         = ":8080"
	templatesDir = "templates"
	staticDir    = "static"
	assetsDir    = "assets"
)

func main() {
	if err := os.MkdirAll(filepath.Dir(scoresFile), 0o755); err != nil {
		log.Fatalf("create db dir: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/tv", serveTemplate("tv.html"))
	mux.HandleFunc("/game", handleGame)
	mux.HandleFunc("/api/scores", handleScores)
	mux.HandleFunc("/api/scores/submit", handleSubmit)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir))))
	mux.Handle("/assets/", http.StripPrefix("/assets/", http.FileServer(http.Dir(assetsDir))))

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

func serveTemplate(name string) http.HandlerFunc {
	path := filepath.Join(templatesDir, name)
	return func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, path)
	}
}
