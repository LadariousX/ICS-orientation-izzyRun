package app

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"
)

type submitResponse struct {
	Rank    int     `json:"rank"`
	Record  bool    `json:"record"`
	NewHigh bool    `json:"newHigh"`
	Top     []Score `json:"top"`
	Total   int     `json:"total"`
}

func handleScores(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	all, err := readScores()
	if err != nil {
		http.Error(w, "read scores", http.StatusInternalServerError)
		log.Printf("read scores: %v", err)
		return
	}
	all = dedupeByName(all)
	sortByScore(all)
	writeJSON(w, map[string]any{
		"top":    topN(all, leaderboardN),
		"total":  len(all),
		"ipinfo": getCurrentIPInfo(),
	})
}

func handleGame(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, filepath.Join(templatesDir, "game.html"))
}

// handlePlayer records the name the player entered on the landing screen and
// captures their IP/location for the TV's "recent players" panel. Called when
// the player taps Start, so the name and geo lookup stay tied to one client.
func handlePlayer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	name := sanitizeName(in.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if isInappropriate(name) {
		http.Error(w, "inappropriate name", http.StatusBadRequest)
		return
	}
	captureIP(getClientIP(r), name)
	w.WriteHeader(http.StatusNoContent)
}

func handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Name  string `json:"name"`
		Score int    `json:"score"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	in.Name = sanitizeName(in.Name)
	if in.Name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if isInappropriate(in.Name) {
		http.Error(w, "inappropriate name", http.StatusBadRequest)
		return
	}
	if in.Score < 0 {
		http.Error(w, "invalid score", http.StatusBadRequest)
		return
	}

	entry := Score{Name: in.Name, Score: in.Score, At: time.Now().Unix()}
	all, newHigh, err := upsertScore(entry)
	if err != nil {
		http.Error(w, "write score", http.StatusInternalServerError)
		log.Printf("upsert score: %v", err)
		return
	}
	sortByScore(all)

	// Rank by name — the player's stored entry is their best, which may predate
	// this submission if they didn't beat it.
	rank := 0
	for i, s := range all {
		if strings.EqualFold(strings.TrimSpace(s.Name), strings.TrimSpace(entry.Name)) {
			rank = i + 1
			break
		}
	}

	writeJSON(w, submitResponse{
		Rank:    rank,
		Record:  rank == 1,
		NewHigh: newHigh,
		Top:     topN(all, leaderboardN),
		Total:   len(all),
	})
}
