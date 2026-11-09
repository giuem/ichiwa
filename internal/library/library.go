// Package library keeps what the user watched and follows, and their
// player settings, in an SQLite database.
package library

import (
	"context"
	"encoding/json"
	"fmt"
	"hongguo/internal/catalog"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// Record is where the user is in a drama.
type Record struct {
	Show     catalog.Show `json:"show"`
	Episode  int          `json:"episode"`  // index into the episodes, from 0
	Position float64      `json:"position"` // seconds into it
	Duration float64      `json:"duration"`
	Updated  time.Time    `json:"updated"`
}

// Library keeps the watch history and the dramas followed. It holds them in
// memory and writes changes through to library.db. It is used on the main
// thread only.
type Library struct {
	History  map[string]*Record
	Follows  map[string]catalog.Show
	Volume   float64
	Speed    float64
	Page     string
	OnTop    bool // the player page keeps the window above others
	Autoplay bool // an episode ending starts the next

	db       *sqlite.DB // nil when the database could not be opened
	order    []string   // followed IDs, latest first
	followed map[string]int64
	watched  map[string]bool // history records not saved yet
	dirty    bool            // settings not saved yet
}

const schema = 1

var migrations = []string{
	`CREATE TABLE history (
		show_id  TEXT PRIMARY KEY,
		show     TEXT NOT NULL, -- catalog.Show as JSON
		episode  INTEGER NOT NULL,
		position REAL NOT NULL,
		duration REAL NOT NULL,
		updated  INTEGER NOT NULL -- Unix milliseconds
	)`,
	`CREATE TABLE follows (
		show_id  TEXT PRIMARY KEY,
		show     TEXT NOT NULL,
		followed INTEGER NOT NULL -- Unix milliseconds, increasing
	)`,
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`PRAGMA user_version = 1`,
}

// Load opens the library in dir, creating it, and importing library.json
// from earlier versions, when needed. If the database cannot be opened it
// logs why and keeps the library in memory only.
func Load(dir string) *Library {
	l := &Library{
		History:  map[string]*Record{},
		Follows:  map[string]catalog.Show{},
		Volume:   1,
		Speed:    1,
		Autoplay: true,
		followed: map[string]int64{},
		watched:  map[string]bool{},
	}
	if err := l.open(dir); err != nil {
		log.Printf("library: %v; nothing will be saved", err)
		if l.db != nil {
			l.db.Close()
			l.db = nil
		}
	}
	if l.Speed <= 0 {
		l.Speed = 1
	}
	return l
}

func (l *Library) open(dir string) error {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, filepath.Join(dir, "library.db"), sqlite.OpenOptions{})
	if err != nil {
		return err
	}
	l.db = db
	if _, err := db.Query(ctx, "PRAGMA journal_mode = WAL"); err != nil {
		return err
	}
	rows, err := db.Query(ctx, "PRAGMA user_version")
	if err != nil {
		return err
	}
	switch version := rows.Values[0][0].(int64); {
	case version == 0:
		stmts := make([]sqlite.Statement, 0, len(migrations))
		for _, sql := range migrations {
			stmts = append(stmts, sqlite.Statement{SQL: sql})
		}
		stmts = append(stmts, importJSON(filepath.Join(dir, "library.json"))...)
		if _, err := db.Transaction(ctx, stmts); err != nil {
			return fmt.Errorf("creating the database: %w", err)
		}
		// Kept, renamed, so it is not imported again.
		old := filepath.Join(dir, "library.json")
		if _, err := os.Stat(old); err == nil {
			_ = os.Rename(old, old+".bak")
		}
	case version > schema:
		return fmt.Errorf("library.db is from a newer version (%d)", version)
	}
	return l.read(ctx)
}

// importJSON returns the statements that copy library.json, if there is
// one, into the new database.
func importJSON(path string) []sqlite.Statement {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var old struct {
		History map[string]*Record      `json:"history"`
		Follows map[string]catalog.Show `json:"follows"`
		Order   []string                `json:"followOrder"`
		Volume  *float64                `json:"volume"`
		Speed   *float64                `json:"speed"`
		Page    string                  `json:"page"`
	}
	if json.Unmarshal(data, &old) != nil {
		log.Printf("library: %s is unreadable; not importing it", path)
		return nil
	}
	var stmts []sqlite.Statement
	for id, r := range old.History {
		if r != nil {
			stmts = append(stmts, historyStatement(id, r))
		}
	}
	// followOrder is latest first; give the latest the largest stamp.
	now := time.Now().UnixMilli()
	for i, id := range old.Order {
		if s, ok := old.Follows[id]; ok {
			stmts = append(stmts, followStatement(s, now-int64(i)))
		}
	}
	if old.Volume != nil {
		stmts = append(stmts, settingStatement("volume", formatFloat(*old.Volume)))
	}
	if old.Speed != nil {
		stmts = append(stmts, settingStatement("speed", formatFloat(*old.Speed)))
	}
	if old.Page != "" {
		stmts = append(stmts, settingStatement("page", old.Page))
	}
	return stmts
}

func (l *Library) read(ctx context.Context) error {
	rows, err := l.db.Query(ctx, "SELECT show_id, show, episode, position, duration, updated FROM history")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		r := &Record{
			Episode:  int(asInt(v[2])),
			Position: asFloat(v[3]),
			Duration: asFloat(v[4]),
			Updated:  time.UnixMilli(asInt(v[5])),
		}
		if json.Unmarshal([]byte(asString(v[1])), &r.Show) == nil {
			l.History[asString(v[0])] = r
		}
	}
	rows, err = l.db.Query(ctx, "SELECT show_id, show, followed FROM follows ORDER BY followed DESC")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		var s catalog.Show
		if json.Unmarshal([]byte(asString(v[1])), &s) == nil {
			id := asString(v[0])
			l.Follows[id] = s
			l.followed[id] = asInt(v[2])
			l.order = append(l.order, id)
		}
	}
	rows, err = l.db.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		value := asString(v[1])
		switch asString(v[0]) {
		case "volume":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				l.Volume = f
			}
		case "speed":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				l.Speed = f
			}
		case "page":
			l.Page = value
		case "on_top":
			l.OnTop = value == "1"
		case "autoplay":
			l.Autoplay = value == "1"
		}
	}
	return nil
}

// write runs statements in one transaction, logging a failure.
func (l *Library) write(stmts ...sqlite.Statement) bool {
	if l.db == nil {
		return true
	}
	if _, err := l.db.Transaction(context.Background(), stmts); err != nil {
		log.Printf("library: saving: %v", err)
		return false
	}
	return true
}

// MarkDirty notes a change to the settings, which Flush saves.
func (l *Library) MarkDirty() { l.dirty = true }

// Flush saves pending changes: progress and settings.
func (l *Library) Flush() {
	var stmts []sqlite.Statement
	for id := range l.watched {
		if r := l.History[id]; r != nil {
			stmts = append(stmts, historyStatement(id, r))
		}
	}
	if l.dirty {
		stmts = append(stmts,
			settingStatement("volume", formatFloat(l.Volume)),
			settingStatement("speed", formatFloat(l.Speed)),
			settingStatement("page", l.Page),
			settingStatement("on_top", onTopValue(l.OnTop)),
			settingStatement("autoplay", onTopValue(l.Autoplay)),
		)
	}
	if len(stmts) > 0 && l.write(stmts...) {
		clear(l.watched)
		l.dirty = false
	}
}

// Close saves pending changes and closes the database.
func (l *Library) Close() {
	l.Flush()
	if l.db != nil {
		l.db.Close()
		l.db = nil
	}
}

// Progress records the position in an episode; it saves later, by Flush.
func (l *Library) Progress(s catalog.Show, episode int, pos, dur float64) {
	w := l.History[s.ID]
	if w == nil {
		w = &Record{}
		l.History[s.ID] = w
	}
	w.Show, w.Episode, w.Position, w.Duration, w.Updated = s, episode, pos, dur, time.Now()
	l.watched[s.ID] = true
}

// Recent returns the history, latest first.
func (l *Library) Recent() []*Record {
	out := make([]*Record, 0, len(l.History))
	for _, w := range l.History {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// Forget removes a drama from the history.
func (l *Library) Forget(id string) {
	delete(l.History, id)
	delete(l.watched, id)
	l.write(sqlite.Statement{SQL: "DELETE FROM history WHERE show_id = ?", Args: []any{id}})
}

// Following reports whether a drama is followed.
func (l *Library) Following(id string) bool {
	_, ok := l.Follows[id]
	return ok
}

// ToggleFollow follows a drama or stops following it.
func (l *Library) ToggleFollow(s catalog.Show) {
	if l.Following(s.ID) {
		delete(l.Follows, s.ID)
		delete(l.followed, s.ID)
		for i, id := range l.order {
			if id == s.ID {
				l.order = append(l.order[:i], l.order[i+1:]...)
				break
			}
		}
		l.write(sqlite.Statement{SQL: "DELETE FROM follows WHERE show_id = ?", Args: []any{s.ID}})
		return
	}
	// Stamps increase even within a millisecond, so the order is kept.
	stamp := time.Now().UnixMilli()
	if len(l.order) > 0 {
		stamp = max(stamp, l.followed[l.order[0]]+1)
	}
	l.Follows[s.ID] = s
	l.followed[s.ID] = stamp
	l.order = append([]string{s.ID}, l.order...)
	l.write(followStatement(s, stamp))
}

// Followed returns the dramas followed, latest first.
func (l *Library) Followed() []catalog.Show {
	out := make([]catalog.Show, 0, len(l.order))
	for _, id := range l.order {
		if s, ok := l.Follows[id]; ok {
			out = append(out, s)
		}
	}
	return out
}

func historyStatement(id string, r *Record) sqlite.Statement {
	show, _ := json.Marshal(r.Show)
	return sqlite.Statement{
		SQL: `INSERT INTO history (show_id, show, episode, position, duration, updated) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT (show_id) DO UPDATE SET show = excluded.show, episode = excluded.episode,
			position = excluded.position, duration = excluded.duration, updated = excluded.updated`,
		Args: []any{id, string(show), r.Episode, finite(r.Position), finite(r.Duration), r.Updated.UnixMilli()},
	}
}

func followStatement(s catalog.Show, stamp int64) sqlite.Statement {
	show, _ := json.Marshal(s)
	return sqlite.Statement{
		SQL:  "INSERT OR REPLACE INTO follows (show_id, show, followed) VALUES (?, ?, ?)",
		Args: []any{s.ID, string(show), stamp},
	}
}

func settingStatement(key, value string) sqlite.Statement {
	return sqlite.Statement{SQL: "INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)", Args: []any{key, value}}
}

func onTopValue(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

// finite turns NaN and infinities, which SQLite cannot bind, into 0; a
// player reports NaN for a duration it does not know yet.
func finite(f float64) float64 {
	if f != f || f > 1e308 || f < -1e308 {
		return 0
	}
	return f
}

func asInt(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

func asFloat(v any) float64 {
	switch n := v.(type) {
	case int64:
		return float64(n)
	case float64:
		return n
	}
	return 0
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}
