package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Record is where the user is in a drama.
type Record struct {
	Show     Show      `json:"show"`
	Episode  int       `json:"episode"`  // index into the episodes, from 0
	Position float64   `json:"position"` // seconds into it
	Duration float64   `json:"duration"`
	Updated  time.Time `json:"updated"`
}

// Library keeps the watch history and the dramas followed, in a JSON file.
// It is used on the main thread only.
type Library struct {
	path    string
	History map[string]*Record `json:"history"`
	Follows map[string]Show    `json:"follows"`
	Order   []string           `json:"followOrder"`
	Volume  float64            `json:"volume"`
	Speed   float64            `json:"speed"`
	Page    string             `json:"page"`
	dirty   bool
}

func loadLibrary(dir string) *Library {
	l := &Library{path: filepath.Join(dir, "library.json"), Volume: 1, Speed: 1}
	if data, err := os.ReadFile(l.path); err == nil {
		_ = json.Unmarshal(data, l)
	}
	if l.History == nil {
		l.History = map[string]*Record{}
	}
	if l.Follows == nil {
		l.Follows = map[string]Show{}
	}
	if l.Speed <= 0 {
		l.Speed = 1
	}
	return l
}

func (l *Library) save() {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return
	}
	tmp := l.path + ".tmp"
	if os.WriteFile(tmp, data, 0o600) == nil {
		_ = os.Rename(tmp, l.path)
	}
	l.dirty = false
}

// Flush saves pending changes.
func (l *Library) Flush() {
	if l.dirty {
		l.save()
	}
}

// Progress records the position in an episode; it saves later, by Flush.
func (l *Library) Progress(s Show, episode int, pos, dur float64) {
	w := l.History[s.ID]
	if w == nil {
		w = &Record{}
		l.History[s.ID] = w
	}
	w.Show, w.Episode, w.Position, w.Duration, w.Updated = s, episode, pos, dur, time.Now()
	l.dirty = true
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
	l.save()
}

// Following reports whether a drama is followed.
func (l *Library) Following(id string) bool {
	_, ok := l.Follows[id]
	return ok
}

// ToggleFollow follows a drama or stops following it.
func (l *Library) ToggleFollow(s Show) {
	if l.Following(s.ID) {
		delete(l.Follows, s.ID)
		for i, id := range l.Order {
			if id == s.ID {
				l.Order = append(l.Order[:i], l.Order[i+1:]...)
				break
			}
		}
	} else {
		l.Follows[s.ID] = s
		l.Order = append([]string{s.ID}, l.Order...)
	}
	l.save()
}

// Followed returns the dramas followed, latest first.
func (l *Library) Followed() []Show {
	out := make([]Show, 0, len(l.Order))
	for _, id := range l.Order {
		if s, ok := l.Follows[id]; ok {
			out = append(out, s)
		}
	}
	return out
}
