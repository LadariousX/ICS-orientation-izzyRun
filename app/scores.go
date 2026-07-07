package app

import (
	"encoding/csv"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
)

const (
	scoresFile   = "db/scores.csv"
	leaderboardN = 5
)

type Score struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
	At    int64  `json:"at"`
}

var scoresMu sync.Mutex

func readScores() ([]Score, error) {
	scoresMu.Lock()
	defer scoresMu.Unlock()
	return readScoresLocked()
}

func readScoresLocked() ([]Score, error) {
	f, err := os.Open(scoresFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]Score, 0, len(rows))
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		score, err := strconv.Atoi(strings.TrimSpace(row[1]))
		if err != nil {
			continue
		}
		var at int64
		if len(row) >= 3 {
			at, _ = strconv.ParseInt(strings.TrimSpace(row[2]), 10, 64)
		}
		out = append(out, Score{Name: row[0], Score: score, At: at})
	}
	return out, nil
}

func appendScore(s Score) ([]Score, error) {
	scoresMu.Lock()
	defer scoresMu.Unlock()

	f, err := os.OpenFile(scoresFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{s.Name, strconv.Itoa(s.Score), strconv.FormatInt(s.At, 10)}); err != nil {
		f.Close()
		return nil, err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return readScoresLocked()
}

func sortByScore(all []Score) {
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].At < all[j].At
	})
}

func topN(all []Score, n int) []Score {
	if len(all) <= n {
		return all
	}
	return all[:n]
}
