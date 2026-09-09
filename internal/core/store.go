package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	_ "github.com/mattn/go-sqlite3"
	"os"
	"path/filepath"
)

type Store struct {
	db   *sql.DB
	Root string
}

func OpenStore(root string) (*Store, error) {
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	root, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", filepath.Join(root, "popchat.db")+"?_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS sessions(id TEXT PRIMARY KEY, data BLOB NOT NULL); CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY, data BLOB NOT NULL); PRAGMA user_version=1;`); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, Root: root}, nil
}
func (s *Store) Save(v *Session) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO sessions(id,data) VALUES(?,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", v.ID, b)
	return e
}
func (s *Store) Delete(id string) error {
	_, e := s.db.Exec("DELETE FROM sessions WHERE id=?", id)
	return e
}
func (s *Store) Load() ([]*Session, error) {
	rows, e := s.db.Query("SELECT data FROM sessions")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []*Session{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			return nil, e
		}
		var v Session
		if e = json.Unmarshal(b, &v); e != nil {
			return nil, fmt.Errorf("会话记录损坏: %w", e)
		}
		normalize(&v)
		out = append(out, &v)
	}
	return out, rows.Err()
}
func (s *Store) Set(key string, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = s.db.Exec("INSERT INTO settings(key,data) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET data=excluded.data", key, b)
	return e
}
func (s *Store) Get(key string, v any) error {
	var b []byte
	e := s.db.QueryRow("SELECT data FROM settings WHERE key=?", key).Scan(&b)
	if e != nil {
		return e
	}
	return json.Unmarshal(b, v)
}
func (s *Store) Close() error { return s.db.Close() }
