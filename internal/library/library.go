// Package library keeps what the user watched and follows, and their
// player settings, in an SQLite database.
package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"

	"github.com/giuem/ichiwa/internal/catalog"
)

// Record is where the user is in a drama.
type Record struct {
	Show     catalog.Show `json:"show"`
	Episode  int          `json:"episode"`  // index into the episodes, from 0
	Position float64      `json:"position"` // seconds into it
	Duration float64      `json:"duration"`
	Updated  time.Time    `json:"updated"`
}

// EpisodeRecord is how far one episode was watched. Finished stays set once
// the episode was watched to its end, watching it again or not.
type EpisodeRecord struct {
	Position float64
	Duration float64
	Finished bool
}

// Fraction is how much of the episode was watched, from 0 to 1.
func (e *EpisodeRecord) Fraction() float64 {
	if e.Finished {
		return 1
	}
	if e.Duration <= 0 {
		return 0
	}
	return min(max(e.Position/e.Duration, 0), 1)
}

// finishedWithin is how close to its end an episode counts as watched.
const finishedWithin = 3 // seconds

type epKey struct {
	show    string
	episode int
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
	Danmaku  bool // comments fly over the video

	// How the comments show: their opacity, from 0.1 to 1, and the steps
	// of the danmaku settings for their speed, the room they take and the
	// size of their text, ResetDanmaku's when not set.
	DanmakuOpacity float64
	DanmakuSpeed   int
	DanmakuArea    int
	DanmakuSize    int

	// Episodes is how far each episode watched got, by drama and then by
	// episode index.
	Episodes map[string]map[int]*EpisodeRecord

	db       *sqlite.DB // nil when the database could not be opened
	order    []string   // followed IDs, latest first
	followed map[string]int64
	watched  map[string]bool // history records not saved yet
	epsDirty map[epKey]bool  // episode records not saved yet
	dirty    bool            // settings not saved yet
}

const schema = 2

// migrations[v] takes the database from version v to v+1.
var migrations = [][]string{{
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
}, {
	`CREATE TABLE episodes (
		show_id  TEXT NOT NULL,
		episode  INTEGER NOT NULL, -- index into the episodes, from 0
		position REAL NOT NULL,
		duration REAL NOT NULL,
		finished INTEGER NOT NULL,
		updated  INTEGER NOT NULL, -- Unix milliseconds
		PRIMARY KEY (show_id, episode)
	)`,
	// The history knew only the episode each drama got to: those before
	// it count as watched, as they did.
	`INSERT INTO episodes (show_id, episode, position, duration, finished, updated)
		WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < 9999)
		SELECT history.show_id, n.i, 0, 0, 1, history.updated FROM history JOIN n ON n.i < history.episode`,
	`PRAGMA user_version = 2`,
}}

// Load opens the library in dir, creating it, and importing library.json
// from earlier versions, when needed. If the database cannot be opened it
// logs why and keeps the library in memory only.
func Load(dir string) *Library {
	l := &Library{
		History:  map[string]*Record{},
		Follows:  map[string]catalog.Show{},
		Episodes: map[string]map[int]*EpisodeRecord{},
		epsDirty: map[epKey]bool{},
		Volume:   1,
		Speed:    1,
		Autoplay: true,
		Danmaku:  true,
		followed: map[string]int64{},
		watched:  map[string]bool{},
	}
	l.ResetDanmaku()
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
	version := int(asInt(rows.Values[0][0]))
	if version > schema {
		return fmt.Errorf("library.db is from a newer version (%d)", version)
	}
	if version < schema {
		var stmts []sqlite.Statement
		for v := version; v < schema; v++ {
			for _, sql := range migrations[v] {
				stmts = append(stmts, sqlite.Statement{SQL: sql})
			}
			if v == 0 { // into the tables of version 1
				stmts = append(stmts, importJSON(filepath.Join(dir, "library.json"))...)
			}
		}
		if _, err := db.Transaction(ctx, stmts); err != nil {
			return fmt.Errorf("updating the database from version %d: %w", version, err)
		}
		// Kept, renamed, so it is not imported again.
		old := filepath.Join(dir, "library.json")
		if _, err := os.Stat(old); err == nil && version == 0 {
			_ = os.Rename(old, old+".bak")
		}
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
	rows, err = l.db.Query(ctx, "SELECT show_id, episode, position, duration, finished FROM episodes")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		l.episode(asString(v[0]), int(asInt(v[1]))).set(asFloat(v[2]), asFloat(v[3]), asInt(v[4]) != 0)
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
		case "danmaku":
			l.Danmaku = value == "1"
		case "danmaku_opacity":
			if f, err := strconv.ParseFloat(value, 64); err == nil {
				l.DanmakuOpacity = f
			}
		case "danmaku_speed":
			l.DanmakuSpeed, _ = strconv.Atoi(value)
		case "danmaku_area":
			l.DanmakuArea, _ = strconv.Atoi(value)
		case "danmaku_size":
			l.DanmakuSize, _ = strconv.Atoi(value)
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

// ResetDanmaku sets the danmaku settings back to their defaults: opaque,
// and the middle step of each other.
func (l *Library) ResetDanmaku() {
	l.DanmakuOpacity, l.DanmakuSpeed, l.DanmakuArea, l.DanmakuSize = 1, 2, 2, 2
	l.dirty = true
}

// DanmakuDefault tells the danmaku settings are their defaults.
func (l *Library) DanmakuDefault() bool {
	return l.DanmakuOpacity == 1 && l.DanmakuSpeed == 2 && l.DanmakuArea == 2 && l.DanmakuSize == 2
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
	for k := range l.epsDirty {
		if e := l.Episodes[k.show][k.episode]; e != nil {
			stmts = append(stmts, episodeStatement(k, e))
		}
	}
	if l.dirty {
		stmts = append(stmts,
			settingStatement("volume", formatFloat(l.Volume)),
			settingStatement("speed", formatFloat(l.Speed)),
			settingStatement("page", l.Page),
			settingStatement("on_top", onTopValue(l.OnTop)),
			settingStatement("autoplay", onTopValue(l.Autoplay)),
			settingStatement("danmaku", onTopValue(l.Danmaku)),
			settingStatement("danmaku_opacity", formatFloat(l.DanmakuOpacity)),
			settingStatement("danmaku_speed", strconv.Itoa(l.DanmakuSpeed)),
			settingStatement("danmaku_area", strconv.Itoa(l.DanmakuArea)),
			settingStatement("danmaku_size", strconv.Itoa(l.DanmakuSize)),
		)
	}
	if len(stmts) > 0 && l.write(stmts...) {
		clear(l.watched)
		clear(l.epsDirty)
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
	e := l.episode(s.ID, episode)
	if dur > 0 && dur == dur { // a duration not known yet keeps the one known
		e.set(finite(pos), dur, e.Finished)
	} else if !e.Finished || pos > 0 {
		e.Position = finite(pos)
	}
	l.epsDirty[epKey{s.ID, episode}] = true
}

// episode returns the record of an episode, making it.
func (l *Library) episode(id string, i int) *EpisodeRecord {
	m := l.Episodes[id]
	if m == nil {
		m = map[int]*EpisodeRecord{}
		l.Episodes[id] = m
	}
	e := m[i]
	if e == nil {
		e = &EpisodeRecord{}
		m[i] = e
	}
	return e
}

// set takes in a position, finishing the episode near its end.
func (e *EpisodeRecord) set(pos, dur float64, finished bool) {
	e.Position, e.Duration = pos, dur
	e.Finished = finished || dur > 0 && pos >= dur-finishedWithin
}

// Episode returns how far episode i of a drama was watched, nil if it
// was not.
func (l *Library) Episode(id string, i int) *EpisodeRecord { return l.Episodes[id][i] }

// Watched reports whether episode i of a drama was watched to its end.
func (l *Library) Watched(id string, i int) bool {
	e := l.Episode(id, i)
	return e != nil && e.Finished
}

// EpisodeStart is where to play episode i of a drama from: where it was
// left, or its start when it was watched to its end or barely begun.
func (l *Library) EpisodeStart(id string, i int) float64 {
	e := l.Episode(id, i)
	if e == nil || e.Position < 5 || e.Duration > 0 && e.Position >= e.Duration-finishedWithin {
		return 0
	}
	return e.Position
}

// Done reports whether the record's drama was watched to its last
// episode's end.
func (r *Record) Done() bool {
	last := r.Show.Episodes - 1
	return r.Show.Episodes > 0 && r.Episode >= last && r.Duration > 0 && r.Position >= r.Duration-finishedWithin
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
	for i := range l.Episodes[id] {
		delete(l.epsDirty, epKey{id, i})
	}
	delete(l.Episodes, id)
	l.write(sqlite.Statement{SQL: "DELETE FROM history WHERE show_id = ?", Args: []any{id}},
		sqlite.Statement{SQL: "DELETE FROM episodes WHERE show_id = ?", Args: []any{id}})
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

func episodeStatement(k epKey, e *EpisodeRecord) sqlite.Statement {
	return sqlite.Statement{
		SQL:  "INSERT OR REPLACE INTO episodes (show_id, episode, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?)",
		Args: []any{k.show, k.episode, finite(e.Position), finite(e.Duration), boolInt(e.Finished), time.Now().UnixMilli()},
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
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
