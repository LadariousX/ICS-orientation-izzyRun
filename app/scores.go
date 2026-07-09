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

// upsertScore records a play, keeping only the highest score per name. The
// whole file is rewritten deduped, so legacy duplicate rows are collapsed the
// first time anyone submits. Returns the deduped list and whether this play beat
// the player's previous best (a new personal high score — true for a first play).
func upsertScore(s Score) ([]Score, bool, error) {
	scoresMu.Lock()
	defer scoresMu.Unlock()

	all, err := readScoresLocked()
	if err != nil {
		return nil, false, err
	}

	prevBest := -1
	for _, x := range all {
		if strings.EqualFold(strings.TrimSpace(x.Name), strings.TrimSpace(s.Name)) && x.Score > prevBest {
			prevBest = x.Score
		}
	}
	newHigh := s.Score > prevBest

	merged := dedupeByName(append(all, s))
	if err := writeScoresLocked(merged); err != nil {
		return nil, false, err
	}
	return merged, newHigh, nil
}

func writeScoresLocked(all []Score) error {
	f, err := os.OpenFile(scoresFile, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	w := csv.NewWriter(f)
	for _, s := range all {
		if err := w.Write([]string{s.Name, strconv.Itoa(s.Score), strconv.FormatInt(s.At, 10)}); err != nil {
			f.Close()
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// dedupeByName collapses entries to one row per name (case-insensitive),
// keeping each name's highest score. First-seen order is preserved; the final
// ordering is decided by sortByScore at read time.
func dedupeByName(all []Score) []Score {
	best := make(map[string]int, len(all))
	out := make([]Score, 0, len(all))
	for _, s := range all {
		key := strings.ToLower(strings.TrimSpace(s.Name))
		if idx, ok := best[key]; ok {
			if s.Score > out[idx].Score {
				out[idx] = s // higher score wins; keep its display name + time
			}
			continue
		}
		best[key] = len(out)
		out = append(out, s)
	}
	return out
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
