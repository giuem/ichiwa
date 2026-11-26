// Package library keeps what the user watched and follows, and their
// player settings, in an SQLite database partitioned by source ID.
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

	"github.com/giuem/ichiwa/internal/source"
)

// LegacySource is the source ID assigned to records migrated from pre-plugin
// schemas (versions 0, 1, and 2).
const LegacySource = "hongguo"

// Record is where the user is in a media item.
type Record struct {
	Source    string      `json:"source"`
	Card      source.Card `json:"card"`
	EpisodeID string      `json:"episodeId,omitempty"`
	Episode   int         `json:"episode"`  // 0-based index into episodes
	Episodes  int         `json:"episodes"` // total episodes when known
	Position  float64     `json:"position"` // seconds into the episode
	Duration  float64     `json:"duration"`
	Updated   time.Time   `json:"updated"`
}

// Ref returns the (source, media ID) reference of this record.
func (r *Record) Ref() source.Ref { return source.Ref{Source: r.Source, ID: r.Card.ID} }

// Follow is a followed media item with its source ID and card snapshot.
type Follow struct {
	Source string      `json:"source"`
	Card   source.Card `json:"card"`
}

// Ref returns the (source, media ID) reference of this follow.
func (f Follow) Ref() source.Ref { return source.Ref{Source: f.Source, ID: f.Card.ID} }

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
	ref       source.Ref
	episodeID string
}

// legacyShow matches the JSON stored in schema 1/2 history and follows tables
// and in library.json.
type legacyShow struct {
	ID       string `json:"ID"`
	Title    string `json:"Title"`
	Desc     string `json:"Desc"`
	Cover    string `json:"Cover"`
	Episodes int    `json:"Episodes"`
	Tag      string `json:"Tag"`
}

func (s legacyShow) toCard(fallbackID string) source.Card {
	id := s.ID
	if id == "" {
		id = fallbackID
	}
	c := source.Card{
		Kind:     source.CardMedia,
		ID:       id,
		Title:    s.Title,
		Subtitle: s.Desc,
		Image:    s.Cover,
		Shape:    source.ShapePortrait,
		Note:     s.Tag,
	}
	if s.Episodes > 0 {
		c.Badges = []string{fmt.Sprintf("全%d集", s.Episodes)}
	}
	return c
}

// Library keeps the watch history and the dramas followed across all sources.
// It holds them in memory and writes changes through to library.db. It is used
// on the main thread only.
type Library struct {
	History  map[source.Ref]*Record
	Follows  map[source.Ref]source.Card
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

	// Episodes is how far each episode watched got, by (source, media ID)
	// and then by episode ID.
	Episodes map[source.Ref]map[string]*EpisodeRecord

	// legacyEpisodes holds index-based episode progress migrated from schema 1/2
	// until the drama is opened and ResolveEpisodes maps indices to episode IDs.
	legacyEpisodes map[source.Ref]map[int]*EpisodeRecord

	db       *sqlite.DB // nil when the database could not be opened
	order    []source.Ref
	followed map[source.Ref]int64
	watched  map[source.Ref]bool // history records not saved yet
	epsDirty map[epKey]bool      // episode records not saved yet
	dirty    bool                // settings not saved yet
}

const schema = 3

// migrations[v] takes the database from version v to v+1 for pure SQL steps.
var migrations = [][]string{{
	`CREATE TABLE history (
		show_id  TEXT PRIMARY KEY,
		show     TEXT NOT NULL,
		episode  INTEGER NOT NULL,
		position REAL NOT NULL,
		duration REAL NOT NULL,
		updated  INTEGER NOT NULL
	)`,
	`CREATE TABLE follows (
		show_id  TEXT PRIMARY KEY,
		show     TEXT NOT NULL,
		followed INTEGER NOT NULL
	)`,
	`CREATE TABLE settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
	`PRAGMA user_version = 1`,
}, {
	`CREATE TABLE episodes (
		show_id  TEXT NOT NULL,
		episode  INTEGER NOT NULL,
		position REAL NOT NULL,
		duration REAL NOT NULL,
		finished INTEGER NOT NULL,
		updated  INTEGER NOT NULL,
		PRIMARY KEY (show_id, episode)
	)`,
	`INSERT INTO episodes (show_id, episode, position, duration, finished, updated)
		WITH RECURSIVE n(i) AS (SELECT 0 UNION ALL SELECT i + 1 FROM n WHERE i < 9999)
		SELECT history.show_id, n.i, 0, 0, 1, history.updated FROM history JOIN n ON n.i < history.episode`,
	`PRAGMA user_version = 2`,
}}

var schema3Tables = []string{
	`CREATE TABLE history (
		source     TEXT NOT NULL,
		media_id   TEXT NOT NULL,
		card       TEXT NOT NULL,
		episode_id TEXT NOT NULL,
		episode    INTEGER NOT NULL,
		episodes   INTEGER NOT NULL,
		position   REAL NOT NULL,
		duration   REAL NOT NULL,
		updated    INTEGER NOT NULL,
		PRIMARY KEY (source, media_id)
	)`,
	`CREATE TABLE follows (
		source   TEXT NOT NULL,
		media_id TEXT NOT NULL,
		card     TEXT NOT NULL,
		followed INTEGER NOT NULL,
		PRIMARY KEY (source, media_id)
	)`,
	`CREATE TABLE episodes (
		source     TEXT NOT NULL,
		media_id   TEXT NOT NULL,
		episode_id TEXT NOT NULL,
		position   REAL NOT NULL,
		duration   REAL NOT NULL,
		finished   INTEGER NOT NULL,
		updated    INTEGER NOT NULL,
		PRIMARY KEY (source, media_id, episode_id)
	)`,
	`CREATE TABLE episodes_legacy (
		source   TEXT NOT NULL,
		media_id TEXT NOT NULL,
		episode  INTEGER NOT NULL,
		position REAL NOT NULL,
		duration REAL NOT NULL,
		finished INTEGER NOT NULL,
		updated  INTEGER NOT NULL,
		PRIMARY KEY (source, media_id, episode)
	)`,
	`CREATE TABLE IF NOT EXISTS settings (
		key   TEXT PRIMARY KEY,
		value TEXT NOT NULL
	)`,
}

// Load opens the library in dir, creating it, migrating older schemas, and
// importing library.json from earlier versions when needed. If the database
// cannot be opened it logs why and keeps the library in memory only.
func Load(dir string) *Library {
	l := &Library{
		History:        map[source.Ref]*Record{},
		Follows:        map[source.Ref]source.Card{},
		Episodes:       map[source.Ref]map[string]*EpisodeRecord{},
		legacyEpisodes: map[source.Ref]map[int]*EpisodeRecord{},
		epsDirty:       map[epKey]bool{},
		Volume:         1,
		Speed:          1,
		Autoplay:       true,
		Danmaku:        true,
		followed:       map[source.Ref]int64{},
		watched:        map[source.Ref]bool{},
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
	if version == 0 {
		oldJSON := filepath.Join(dir, "library.json")
		if _, err := os.Stat(oldJSON); err == nil {
			// Import legacy JSON into schema 1, then step through 1 -> 2 -> 3.
			var stmts []sqlite.Statement
			for _, sql := range migrations[0] {
				stmts = append(stmts, sqlite.Statement{SQL: sql})
			}
			stmts = append(stmts, importJSON(oldJSON)...)
			if _, err := db.Transaction(ctx, stmts); err != nil {
				return fmt.Errorf("importing library.json: %w", err)
			}
			_ = os.Rename(oldJSON, oldJSON+".bak")
			version = 1
		} else {
			var stmts []sqlite.Statement
			for _, sql := range schema3Tables {
				stmts = append(stmts, sqlite.Statement{SQL: sql})
			}
			stmts = append(stmts, sqlite.Statement{SQL: "PRAGMA user_version = 3"})
			if _, err := db.Transaction(ctx, stmts); err != nil {
				return fmt.Errorf("creating database: %w", err)
			}
			version = schema
		}
	}
	if version == 1 {
		var stmts []sqlite.Statement
		for _, sql := range migrations[1] {
			stmts = append(stmts, sqlite.Statement{SQL: sql})
		}
		if _, err := db.Transaction(ctx, stmts); err != nil {
			return fmt.Errorf("updating the database from version 1: %w", err)
		}
		version = 2
	}
	if version == 2 {
		if err := migrateV2ToV3(ctx, db); err != nil {
			return fmt.Errorf("updating the database from version 2: %w", err)
		}
	}
	return l.read(ctx)
}

func migrateV2ToV3(ctx context.Context, db *sqlite.DB) error {
	histRows, err := db.Query(ctx, "SELECT show_id, show, episode, position, duration, updated FROM history")
	if err != nil {
		return err
	}
	followRows, err := db.Query(ctx, "SELECT show_id, show, followed FROM follows")
	if err != nil {
		return err
	}
	epRows, err := db.Query(ctx, "SELECT show_id, episode, position, duration, finished, updated FROM episodes")
	if err != nil {
		return err
	}

	var stmts []sqlite.Statement
	stmts = append(stmts,
		sqlite.Statement{SQL: "DROP TABLE history"},
		sqlite.Statement{SQL: "DROP TABLE follows"},
		sqlite.Statement{SQL: "DROP TABLE episodes"},
	)
	for _, sql := range schema3Tables {
		stmts = append(stmts, sqlite.Statement{SQL: sql})
	}
	for _, v := range histRows.Values {
		showID := asString(v[0])
		var ls legacyShow
		_ = json.Unmarshal([]byte(asString(v[1])), &ls)
		cardBytes, _ := json.Marshal(ls.toCard(showID))
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO history (source, media_id, card, episode_id, episode, episodes, position, duration, updated)
				VALUES (?, ?, ?, '', ?, ?, ?, ?, ?)`,
			Args: []any{LegacySource, showID, string(cardBytes), int(asInt(v[2])), ls.Episodes, finite(asFloat(v[3])), finite(asFloat(v[4])), asInt(v[5])},
		})
	}
	for _, v := range followRows.Values {
		showID := asString(v[0])
		var ls legacyShow
		_ = json.Unmarshal([]byte(asString(v[1])), &ls)
		cardBytes, _ := json.Marshal(ls.toCard(showID))
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO follows (source, media_id, card, followed) VALUES (?, ?, ?, ?)`,
			Args: []any{LegacySource, showID, string(cardBytes), asInt(v[2])},
		})
	}
	for _, v := range epRows.Values {
		stmts = append(stmts, sqlite.Statement{
			SQL:  `INSERT INTO episodes_legacy (source, media_id, episode, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{LegacySource, asString(v[0]), int(asInt(v[1])), finite(asFloat(v[2])), finite(asFloat(v[3])), int(asInt(v[4])), asInt(v[5])},
		})
	}
	stmts = append(stmts, sqlite.Statement{SQL: "PRAGMA user_version = 3"})
	_, err = db.Transaction(ctx, stmts)
	return err
}

// importJSON returns the statements that copy library.json into schema 1 tables.
func importJSON(path string) []sqlite.Statement {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	type legacyRecord struct {
		Show     legacyShow `json:"show"`
		Episode  int        `json:"episode"`
		Position float64    `json:"position"`
		Duration float64    `json:"duration"`
		Updated  time.Time  `json:"updated"`
	}
	var old struct {
		History map[string]*legacyRecord `json:"history"`
		Follows map[string]legacyShow    `json:"follows"`
		Order   []string                 `json:"followOrder"`
		Volume  *float64                 `json:"volume"`
		Speed   *float64                 `json:"speed"`
		Page    string                   `json:"page"`
	}
	if json.Unmarshal(data, &old) != nil {
		log.Printf("library: %s is unreadable; not importing it", path)
		return nil
	}
	var stmts []sqlite.Statement
	for id, r := range old.History {
		if r == nil {
			continue
		}
		showBytes, _ := json.Marshal(r.Show)
		stmts = append(stmts, sqlite.Statement{
			SQL: `INSERT INTO history (show_id, show, episode, position, duration, updated) VALUES (?, ?, ?, ?, ?, ?)
				ON CONFLICT (show_id) DO UPDATE SET show = excluded.show, episode = excluded.episode,
				position = excluded.position, duration = excluded.duration, updated = excluded.updated`,
			Args: []any{id, string(showBytes), r.Episode, finite(r.Position), finite(r.Duration), r.Updated.UnixMilli()},
		})
	}
	now := time.Now().UnixMilli()
	for i, id := range old.Order {
		if s, ok := old.Follows[id]; ok {
			showBytes, _ := json.Marshal(s)
			stmts = append(stmts, sqlite.Statement{
				SQL:  "INSERT OR REPLACE INTO follows (show_id, show, followed) VALUES (?, ?, ?)",
				Args: []any{id, string(showBytes), now - int64(i)},
			})
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
	rows, err := l.db.Query(ctx, "SELECT source, media_id, card, episode_id, episode, episodes, position, duration, updated FROM history")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		srcID, mediaID := asString(v[0]), asString(v[1])
		r := &Record{
			Source:    srcID,
			EpisodeID: asString(v[3]),
			Episode:   int(asInt(v[4])),
			Episodes:  int(asInt(v[5])),
			Position:  asFloat(v[6]),
			Duration:  asFloat(v[7]),
			Updated:   time.UnixMilli(asInt(v[8])),
		}
		if json.Unmarshal([]byte(asString(v[2])), &r.Card) == nil {
			if r.Card.ID == "" {
				r.Card.ID = mediaID
			}
			l.History[source.Ref{Source: srcID, ID: mediaID}] = r
		}
	}
	rows, err = l.db.Query(ctx, "SELECT source, media_id, card, followed FROM follows ORDER BY followed DESC")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		var c source.Card
		if json.Unmarshal([]byte(asString(v[2])), &c) == nil {
			ref := source.Ref{Source: asString(v[0]), ID: asString(v[1])}
			if c.ID == "" {
				c.ID = ref.ID
			}
			l.Follows[ref] = c
			l.followed[ref] = asInt(v[3])
			l.order = append(l.order, ref)
		}
	}
	rows, err = l.db.Query(ctx, "SELECT source, media_id, episode_id, position, duration, finished FROM episodes")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		ref := source.Ref{Source: asString(v[0]), ID: asString(v[1])}
		l.episode(ref, asString(v[2])).set(asFloat(v[3]), asFloat(v[4]), asInt(v[5]) != 0)
	}
	rows, err = l.db.Query(ctx, "SELECT source, media_id, episode, position, duration, finished FROM episodes_legacy")
	if err != nil {
		return err
	}
	for _, v := range rows.Values {
		ref := source.Ref{Source: asString(v[0]), ID: asString(v[1])}
		idx := int(asInt(v[2]))
		m := l.legacyEpisodes[ref]
		if m == nil {
			m = map[int]*EpisodeRecord{}
			l.legacyEpisodes[ref] = m
		}
		e := &EpisodeRecord{}
		e.set(asFloat(v[3]), asFloat(v[4]), asInt(v[5]) != 0)
		m[idx] = e
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
	for ref := range l.watched {
		if r := l.History[ref]; r != nil {
			stmts = append(stmts, historyStatement(ref, r))
		}
	}
	for k := range l.epsDirty {
		if e := l.Episodes[k.ref][k.episodeID]; e != nil {
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
func (l *Library) Progress(sourceID string, card source.Card, episodeID string, episode, totalEpisodes int, pos, dur float64) {
	ref := source.Ref{Source: sourceID, ID: card.ID}
	w := l.History[ref]
	if w == nil {
		w = &Record{}
		l.History[ref] = w
	}
	if totalEpisodes == 0 && w.Episodes > 0 {
		totalEpisodes = w.Episodes
	}
	w.Source = sourceID
	w.Card = card
	w.EpisodeID = episodeID
	w.Episode = episode
	w.Episodes = totalEpisodes
	w.Position = pos
	w.Duration = dur
	w.Updated = time.Now()
	l.watched[ref] = true

	if episodeID != "" {
		e := l.episode(ref, episodeID)
		if dur > 0 && dur == dur { // a duration not known yet keeps the one known
			e.set(finite(pos), dur, e.Finished)
		} else if !e.Finished || pos > 0 {
			e.Position = finite(pos)
		}
		l.epsDirty[epKey{ref, episodeID}] = true
	}
}

// ResolveEpisodes converts any legacy index-based episode progress for ref into
// episode-ID progress using episodeIDs (in playback order) and fills a missing
// EpisodeID on the history record.
func (l *Library) ResolveEpisodes(ref source.Ref, episodeIDs []string) {
	if len(episodeIDs) == 0 {
		return
	}
	var stmts []sqlite.Statement
	if legacy := l.legacyEpisodes[ref]; len(legacy) > 0 {
		var resolved []int
		for idx, old := range legacy {
			if idx < 0 || idx >= len(episodeIDs) || episodeIDs[idx] == "" {
				continue
			}
			epID := episodeIDs[idx]
			cur := l.episode(ref, epID)
			if cur.Duration == 0 && old.Duration > 0 {
				cur.Duration = old.Duration
			}
			if cur.Position == 0 && old.Position > 0 {
				cur.Position = old.Position
			}
			if old.Finished {
				cur.Finished = true
			}
			stmts = append(stmts,
				episodeStatement(epKey{ref, epID}, cur),
				sqlite.Statement{
					SQL:  "DELETE FROM episodes_legacy WHERE source = ? AND media_id = ? AND episode = ?",
					Args: []any{ref.Source, ref.ID, idx},
				},
			)
			resolved = append(resolved, idx)
		}
		for _, idx := range resolved {
			delete(legacy, idx)
		}
		if len(legacy) == 0 {
			delete(l.legacyEpisodes, ref)
		}
	}
	if rec := l.History[ref]; rec != nil && rec.EpisodeID == "" && rec.Episode >= 0 && rec.Episode < len(episodeIDs) {
		if epID := episodeIDs[rec.Episode]; epID != "" {
			rec.EpisodeID = epID
			if rec.Episodes == 0 {
				rec.Episodes = len(episodeIDs)
			}
			stmts = append(stmts, historyStatement(ref, rec))
		}
	}
	if len(stmts) > 0 {
		l.write(stmts...)
	}
}

// episode returns the record of an episode, making it.
func (l *Library) episode(ref source.Ref, episodeID string) *EpisodeRecord {
	m := l.Episodes[ref]
	if m == nil {
		m = map[string]*EpisodeRecord{}
		l.Episodes[ref] = m
	}
	e := m[episodeID]
	if e == nil {
		e = &EpisodeRecord{}
		m[episodeID] = e
	}
	return e
}

// set takes in a position, finishing the episode near its end.
func (e *EpisodeRecord) set(pos, dur float64, finished bool) {
	e.Position, e.Duration = pos, dur
	e.Finished = finished || dur > 0 && pos >= dur-finishedWithin
}

// Episode returns how far episodeID of ref was watched, or nil if not watched.
func (l *Library) Episode(ref source.Ref, episodeID string) *EpisodeRecord {
	return l.Episodes[ref][episodeID]
}

// Watched reports whether episodeID of ref was watched to its end.
func (l *Library) Watched(ref source.Ref, episodeID string) bool {
	e := l.Episode(ref, episodeID)
	return e != nil && e.Finished
}

// EpisodeStart is where to play episodeID of ref from: where it was left, or
// its start when it was watched to its end or barely begun.
func (l *Library) EpisodeStart(ref source.Ref, episodeID string) float64 {
	e := l.Episode(ref, episodeID)
	if e == nil || e.Position < 5 || e.Duration > 0 && e.Position >= e.Duration-finishedWithin {
		return 0
	}
	return e.Position
}

// Done reports whether the record's media was watched to its last episode's end.
func (r *Record) Done() bool {
	last := r.Episodes - 1
	return r.Episodes > 0 && r.Episode >= last && r.Duration > 0 && r.Position >= r.Duration-finishedWithin
}

// Recent returns the history across all sources, latest first.
func (l *Library) Recent() []*Record {
	out := make([]*Record, 0, len(l.History))
	for _, w := range l.History {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Updated.After(out[j].Updated) })
	return out
}

// Forget removes a media item from the history and clears its episode progress.
func (l *Library) Forget(ref source.Ref) {
	delete(l.History, ref)
	delete(l.watched, ref)
	for epID := range l.Episodes[ref] {
		delete(l.epsDirty, epKey{ref, epID})
	}
	delete(l.Episodes, ref)
	delete(l.legacyEpisodes, ref)
	l.write(
		sqlite.Statement{SQL: "DELETE FROM history WHERE source = ? AND media_id = ?", Args: []any{ref.Source, ref.ID}},
		sqlite.Statement{SQL: "DELETE FROM episodes WHERE source = ? AND media_id = ?", Args: []any{ref.Source, ref.ID}},
		sqlite.Statement{SQL: "DELETE FROM episodes_legacy WHERE source = ? AND media_id = ?", Args: []any{ref.Source, ref.ID}},
	)
}

// Following reports whether ref is followed.
func (l *Library) Following(ref source.Ref) bool {
	_, ok := l.Follows[ref]
	return ok
}

// ToggleFollow follows a media item or stops following it.
func (l *Library) ToggleFollow(sourceID string, card source.Card) {
	ref := source.Ref{Source: sourceID, ID: card.ID}
	if l.Following(ref) {
		delete(l.Follows, ref)
		delete(l.followed, ref)
		for i, r := range l.order {
			if r == ref {
				l.order = append(l.order[:i], l.order[i+1:]...)
				break
			}
		}
		l.write(sqlite.Statement{SQL: "DELETE FROM follows WHERE source = ? AND media_id = ?", Args: []any{ref.Source, ref.ID}})
		return
	}
	// Stamps increase even within a millisecond, so the order is kept.
	stamp := time.Now().UnixMilli()
	if len(l.order) > 0 {
		stamp = max(stamp, l.followed[l.order[0]]+1)
	}
	l.Follows[ref] = card
	l.followed[ref] = stamp
	l.order = append([]source.Ref{ref}, l.order...)
	l.write(followStatement(ref, card, stamp))
}

// Followed returns the followed media items across all sources, latest first.
func (l *Library) Followed() []Follow {
	out := make([]Follow, 0, len(l.order))
	for _, ref := range l.order {
		if c, ok := l.Follows[ref]; ok {
			out = append(out, Follow{Source: ref.Source, Card: c})
		}
	}
	return out
}

func historyStatement(ref source.Ref, r *Record) sqlite.Statement {
	card, _ := json.Marshal(r.Card)
	return sqlite.Statement{
		SQL: `INSERT INTO history (source, media_id, card, episode_id, episode, episodes, position, duration, updated)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (source, media_id) DO UPDATE SET card = excluded.card, episode_id = excluded.episode_id,
			episode = excluded.episode, episodes = excluded.episodes, position = excluded.position,
			duration = excluded.duration, updated = excluded.updated`,
		Args: []any{ref.Source, ref.ID, string(card), r.EpisodeID, r.Episode, r.Episodes, finite(r.Position), finite(r.Duration), r.Updated.UnixMilli()},
	}
}

func episodeStatement(k epKey, e *EpisodeRecord) sqlite.Statement {
	return sqlite.Statement{
		SQL:  "INSERT OR REPLACE INTO episodes (source, media_id, episode_id, position, duration, finished, updated) VALUES (?, ?, ?, ?, ?, ?, ?)",
		Args: []any{k.ref.Source, k.ref.ID, k.episodeID, finite(e.Position), finite(e.Duration), boolInt(e.Finished), time.Now().UnixMilli()},
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func followStatement(ref source.Ref, card source.Card, stamp int64) sqlite.Statement {
	data, _ := json.Marshal(card)
	return sqlite.Statement{
		SQL:  "INSERT OR REPLACE INTO follows (source, media_id, card, followed) VALUES (?, ?, ?, ?)",
		Args: []any{ref.Source, ref.ID, string(data), stamp},
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
