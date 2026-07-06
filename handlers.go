package main

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"time"
)

type submitResponse struct {
	Rank   int     `json:"rank"`
	Record bool    `json:"record"`
	Top    []Score `json:"top"`
	Total  int     `json:"total"`
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
	sortByScore(all)
	writeJSON(w, map[string]any{
		"top":    topN(all, leaderboardN),
		"total":  len(all),
		"ipinfo": getCurrentIPInfo(),
	})
}

func handleGame(w http.ResponseWriter, r *http.Request) {
	captureIP(getClientIP(r))
	http.ServeFile(w, r, filepath.Join(templatesDir, "game.html"))
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
	if in.Score < 0 {
		http.Error(w, "invalid score", http.StatusBadRequest)
		return
	}

	entry := Score{Name: in.Name, Score: in.Score, At: time.Now().Unix()}
	all, err := appendScore(entry)
	if err != nil {
		http.Error(w, "write score", http.StatusInternalServerError)
		log.Printf("append score: %v", err)
		return
	}
	sortByScore(all)

	rank := 0
	for i, s := range all {
		if s.Name == entry.Name && s.Score == entry.Score && s.At == entry.At {
			rank = i + 1
			break
		}
	}

	writeJSON(w, submitResponse{
		Rank:   rank,
		Record: rank == 1,
		Top:    topN(all, leaderboardN),
		Total:  len(all),
	})
}
