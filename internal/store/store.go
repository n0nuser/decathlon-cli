// Package store is the local watch database: watches, the last state each
// watch saw, and the events it produced. SQLite through modernc.org/sqlite,
// so the binary stays cgo-free and static.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

const (
	KindSearch  = "search"
	KindProduct = "product"
)

type Store struct{ db *sql.DB }

// Watch is one saved target. Target is kind-specific JSON.
type Watch struct {
	ID          int64           `json:"-"`
	Name        string          `json:"name"`
	Kind        string          `json:"kind"`
	Target      json.RawMessage `json:"target"`
	Sinks       []string        `json:"sinks"`
	Interval    time.Duration   `json:"interval"`
	CreatedAt   time.Time       `json:"created_at"`
	LastCheckAt time.Time       `json:"last_check_at,omitzero"`
	Baselined   bool            `json:"baselined"`
}

// Seen is the last state of one key (a product tile, or one SKU) under a watch.
type Seen struct {
	Key      string
	Snapshot json.RawMessage
}

// Event is one change a check found. Product, SKU, Store and Change are
// JSON so each event type carries only what applies to it.
type Event struct {
	ID      int64           `json:"-"`
	Type    string          `json:"type"`
	Watch   string          `json:"watch"`
	At      time.Time       `json:"at"`
	Product json.RawMessage `json:"product,omitempty"`
	SKU     json.RawMessage `json:"sku,omitempty"`
	Store   json.RawMessage `json:"store,omitempty"`
	Change  json.RawMessage `json:"change,omitempty"`
}

const schema = `
CREATE TABLE IF NOT EXISTS watches (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL,
  target TEXT NOT NULL,
  sinks TEXT NOT NULL DEFAULT '[]',
  interval_sec INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  last_check_at TEXT,
  baselined INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS seen (
  watch_id INTEGER NOT NULL REFERENCES watches(id) ON DELETE CASCADE,
  key TEXT NOT NULL,
  snapshot TEXT NOT NULL,
  first_seen TEXT NOT NULL,
  last_seen TEXT NOT NULL,
  PRIMARY KEY (watch_id, key)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY,
  watch TEXT NOT NULL,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  body TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS events_watch_at ON events(watch, at);
`

// Open creates the database (0600, in a 0700 directory) if needed.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600); err == nil {
		f.Close()
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("state database %s: %w", path, err)
	}
	_ = os.Chmod(path, 0o600)
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

var ErrNotFound = errors.New("no such watch")
var ErrExists = errors.New("a watch with that name already exists")

const ts = time.RFC3339Nano

func (s *Store) AddWatch(ctx context.Context, w *Watch) error {
	sinks, _ := json.Marshal(w.Sinks)
	w.CreatedAt = time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO watches(name, kind, target, sinks, interval_sec, created_at) VALUES (?,?,?,?,?,?)`,
		w.Name, w.Kind, string(w.Target), string(sinks), int64(w.Interval/time.Second), w.CreatedAt.Format(ts))
	if err != nil {
		if s.exists(ctx, w.Name) {
			return ErrExists
		}
		return err
	}
	w.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) exists(ctx context.Context, name string) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM watches WHERE name = ?`, name).Scan(&n)
	return n > 0
}

func (s *Store) RemoveWatch(ctx context.Context, name string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM watches WHERE name = ?`, name)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM events WHERE watch = ?`, name)
	return err
}

func (s *Store) Watches(ctx context.Context) ([]Watch, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, kind, target, sinks, interval_sec, created_at, coalesce(last_check_at, ''), baselined FROM watches ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Watch{}
	for rows.Next() {
		var w Watch
		var target, sinks, created, last string
		var iv int64
		if err := rows.Scan(&w.ID, &w.Name, &w.Kind, &target, &sinks, &iv, &created, &last, &w.Baselined); err != nil {
			return nil, err
		}
		w.Target = json.RawMessage(target)
		_ = json.Unmarshal([]byte(sinks), &w.Sinks)
		w.Interval = time.Duration(iv) * time.Second
		w.CreatedAt, _ = time.Parse(ts, created)
		if last != "" {
			w.LastCheckAt, _ = time.Parse(ts, last)
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (s *Store) Watch(ctx context.Context, name string) (Watch, error) {
	ws, err := s.Watches(ctx)
	if err != nil {
		return Watch{}, err
	}
	for _, w := range ws {
		if w.Name == name {
			return w, nil
		}
	}
	return Watch{}, ErrNotFound
}

func (s *Store) SeenFor(ctx context.Context, watchID int64) (map[string]Seen, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, snapshot FROM seen WHERE watch_id = ?`, watchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Seen{}
	for rows.Next() {
		var k, snap string
		if err := rows.Scan(&k, &snap); err != nil {
			return nil, err
		}
		out[k] = Seen{Key: k, Snapshot: json.RawMessage(snap)}
	}
	return out, rows.Err()
}

// CommitCheck records a check's outcome in one transaction: new state,
// removed keys, events, and the check time. The CLI calls it only after the
// events went to stdout and sinks, so a crash re-reports instead of losing.
func (s *Store) CommitCheck(ctx context.Context, w Watch, upserts []Seen, removed []string, events []Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(ts)
	for _, u := range upserts {
		if _, err := tx.ExecContext(ctx, `INSERT INTO seen(watch_id, key, snapshot, first_seen, last_seen) VALUES (?,?,?,?,?)
			ON CONFLICT(watch_id, key) DO UPDATE SET snapshot = excluded.snapshot, last_seen = excluded.last_seen`, w.ID, u.Key, string(u.Snapshot), now, now); err != nil {
			return err
		}
	}
	for _, k := range removed {
		if _, err := tx.ExecContext(ctx, `DELETE FROM seen WHERE watch_id = ? AND key = ?`, w.ID, k); err != nil {
			return err
		}
	}
	for _, e := range events {
		body, _ := json.Marshal(e)
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(watch, type, at, body) VALUES (?,?,?,?)`, e.Watch, e.Type, e.At.Format(ts), string(body)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE watches SET last_check_at = ?, baselined = 1 WHERE id = ?`, now, w.ID); err != nil {
		return err
	}
	return tx.Commit()
}

// Events returns stored events, newest first.
func (s *Store) Events(ctx context.Context, watch string, since time.Time, limit int) ([]Event, error) {
	q := `SELECT id, body FROM events WHERE at >= ?`
	args := []any{since.UTC().Format(ts)}
	if watch != "" {
		q += ` AND watch = ?`
		args = append(args, watch)
	}
	q += ` ORDER BY at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		var id int64
		var body string
		if err := rows.Scan(&id, &body); err != nil {
			return nil, err
		}
		var e Event
		if json.Unmarshal([]byte(body), &e) == nil {
			e.ID = id
			out = append(out, e)
		}
	}
	return out, rows.Err()
}
