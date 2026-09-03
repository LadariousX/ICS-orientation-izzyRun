package app

import (
	"database/sql"
	"encoding/csv"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	_ "modernc.org/sqlite"
)

const (
	dbFile = "db/scores.db"
	// legacyCSV is the pre-SQLite store. It's imported once, on the first run
	// against an empty table, then left on disk untouched.
	legacyCSV    = "db/scores.csv"
	leaderboardN = 5

	schema = `CREATE TABLE IF NOT EXISTS scores (
		name_key TEXT PRIMARY KEY,
		name     TEXT    NOT NULL,
		score    INTEGER NOT NULL,
		at       INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS scores_rank ON scores (score DESC, at ASC);`
)

type Score struct {
	Name  string `json:"name"`
	Score int    `json:"score"`
	At    int64  `json:"at"`
}

var db *sql.DB

// openDB creates the db directory, opens db/scores.db, applies the schema, and
// imports the legacy CSV if the table is still empty.
//
// One row per player, keyed on the lowercased name, so "keep each player's
// highest score" is enforced by the primary key + UPSERT rather than by
// rewriting and deduping the whole store on every submission. WAL plus a busy
// timeout is what makes concurrent submissions from a room full of phones safe.
func openDB() error {
	if err := os.MkdirAll(filepath.Dir(dbFile), 0o755); err != nil {
		return fmt.Errorf("create db dir: %w", err)
	}
	handle, err := sql.Open("sqlite", "file:"+dbFile+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return fmt.Errorf("open %s: %w", dbFile, err)
	}
	if err := handle.Ping(); err != nil {
		return fmt.Errorf("ping %s: %w", dbFile, err)
	}
	if _, err := handle.Exec(schema); err != nil {
		return fmt.Errorf("apply schema: %w", err)
	}
	db = handle
	return importLegacyCSV()
}

// importLegacyCSV loads db/scores.csv into an empty scores table so the
// leaderboard survives the move to SQLite. A non-empty table means the import
// already happened, which makes this a no-op on every subsequent start.
func importLegacyCSV() error {
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM scores`).Scan(&n); err != nil {
		return fmt.Errorf("count scores: %w", err)
	}
	if n > 0 {
		return nil
	}

	rows, err := readLegacyCSV()
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	for _, s := range rows {
		if _, _, err := upsertScore(s); err != nil {
			return fmt.Errorf("import %q: %w", s.Name, err)
		}
	}
	log.Printf("imported %d rows from %s into %s", len(rows), legacyCSV, dbFile)
	return nil
}

func readLegacyCSV() ([]Score, error) {
	f, err := os.Open(legacyCSV)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]Score, 0, len(records))
	for _, row := range records {
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

func nameKey(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// upsertScore records a play, keeping only the player's highest score. Returns
// the player's stored best after the write and whether this play beat their
// previous best (true for a first play).
func upsertScore(s Score) (best Score, newHigh bool, err error) {
	tx, err := db.Begin()
	if err != nil {
		return Score{}, false, err
	}
	defer tx.Rollback()

	key := nameKey(s.Name)

	prevBest := -1
	err = tx.QueryRow(`SELECT score FROM scores WHERE name_key = ?`, key).Scan(&prevBest)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Score{}, false, err
	}
	newHigh = s.Score > prevBest

	// Display name and timestamp travel with the winning score, matching the old
	// CSV dedupe ("higher score wins; keep its display name + time").
	_, err = tx.Exec(`
		INSERT INTO scores (name_key, name, score, at) VALUES (?, ?, ?, ?)
		ON CONFLICT(name_key) DO UPDATE SET
			name  = excluded.name,
			score = excluded.score,
			at    = excluded.at
		WHERE excluded.score > scores.score`,
		key, strings.TrimSpace(s.Name), s.Score, s.At)
	if err != nil {
		return Score{}, false, err
	}

	if err = tx.QueryRow(`SELECT name, score, at FROM scores WHERE name_key = ?`, key).
		Scan(&best.Name, &best.Score, &best.At); err != nil {
		return Score{}, false, err
	}
	return best, newHigh, tx.Commit()
}

// leaderboard returns the top N players and the total number of players.
func leaderboard() ([]Score, int, error) {
	rows, err := db.Query(
		`SELECT name, score, at FROM scores ORDER BY score DESC, at ASC LIMIT ?`,
		leaderboardN)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	top := make([]Score, 0, leaderboardN)
	for rows.Next() {
		var s Score
		if err := rows.Scan(&s.Name, &s.Score, &s.At); err != nil {
			return nil, 0, err
		}
		top = append(top, s)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM scores`).Scan(&total); err != nil {
		return nil, 0, err
	}
	return top, total, nil
}

// rankOf returns the 1-based leaderboard position of s, ordered by score
// descending then earliest timestamp — the same ordering leaderboard() uses.
func rankOf(s Score) (int, error) {
	var ahead int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM scores WHERE score > ? OR (score = ? AND at < ?)`,
		s.Score, s.Score, s.At).Scan(&ahead)
	if err != nil {
		return 0, err
	}
	return ahead + 1, nil
}
