package plugin

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// MaxLogBytes is how much of its log a plugin keeps (§8).
const MaxLogBytes = 1 << 20

// maxLogLine caps one entry, so one huge line cannot push out the rest.
const maxLogLine = 16 << 10

// logEntryCost is what an entry counts besides its text, so that empty
// lines fill the log too.
const logEntryCost = 64

// LogEntry is one line of a plugin's log.
type LogEntry struct {
	Time  time.Time
	Level string // log, info, warn, error, debug; host for the host's own lines
	Text  string
}

func (e LogEntry) String() string {
	return fmt.Sprintf("%s [%s] %s", e.Time.Format("15:04:05.000"), e.Level, e.Text)
}

// Logs is a plugin's log: the newest entries up to MaxLogBytes, counting
// each entry's text and logEntryCost, in a ring.
type Logs struct {
	mu      sync.Mutex
	entries []LogEntry
	start   int // index of the oldest entry
	size    int // bytes held, by entrySize
	watch   map[int]func(LogEntry)
	nextID  int
}

func (l *Logs) add(level, text string) {
	if len(text) > maxLogLine {
		cut := maxLogLine
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut] + "…（已截断）"
	}
	e := LogEntry{Time: time.Now(), Level: level, Text: text}
	l.mu.Lock()
	l.entries = append(l.entries, e)
	l.size += entrySize(e)
	for l.size > MaxLogBytes && l.start < len(l.entries) {
		l.size -= entrySize(l.entries[l.start])
		l.entries[l.start] = LogEntry{}
		l.start++
	}
	// Compact once the dropped part dominates.
	if l.start > 1024 && l.start*2 > len(l.entries) {
		l.entries = append([]LogEntry(nil), l.entries[l.start:]...)
		l.start = 0
	}
	watchers := make([]func(LogEntry), 0, len(l.watch))
	for _, w := range l.watch {
		watchers = append(watchers, w)
	}
	l.mu.Unlock()
	for _, w := range watchers {
		w(e)
	}
}

func entrySize(e LogEntry) int { return len(e.Text) + logEntryCost }

// Hostf adds a line of the host's own about the plugin.
func (l *Logs) Hostf(format string, args ...any) { l.add("host", fmt.Sprintf(format, args...)) }

// Entries returns the entries held, oldest first.
func (l *Logs) Entries() []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]LogEntry(nil), l.entries[l.start:]...)
}

// Tail returns the newest n entries held, oldest first.
func (l *Logs) Tail(n int) []LogEntry {
	l.mu.Lock()
	defer l.mu.Unlock()
	held := l.entries[l.start:]
	if len(held) > n {
		held = held[len(held)-n:]
	}
	return append([]LogEntry(nil), held...)
}

// Text returns the log as text, one entry a line, to view or export.
func (l *Logs) Text() string {
	var b strings.Builder
	for _, e := range l.Entries() {
		b.WriteString(e.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// Watch calls fn with every entry added from now on, until stop is
// called. fn runs on the goroutine that logs, so it must not block.
func (l *Logs) Watch(fn func(LogEntry)) (stop func()) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.watch == nil {
		l.watch = map[int]func(LogEntry){}
	}
	id := l.nextID
	l.nextID++
	l.watch[id] = fn
	return func() {
		l.mu.Lock()
		delete(l.watch, id)
		l.mu.Unlock()
	}
}
