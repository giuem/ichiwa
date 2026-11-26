package app

import (
	"github.com/giuem/ichiwa/internal/library"
	"github.com/giuem/ichiwa/internal/source"
)

// The source protocol's and the library's types, by the names the views use.
type (
	Card      = source.Card
	Media     = source.Media
	Season    = source.Season
	Section   = source.Section
	FilterRow = source.FilterRow
	Record    = library.Record
)

var errText = source.Message
