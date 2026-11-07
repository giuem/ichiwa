package app

import (
	"hongguo/internal/catalog"
	"hongguo/internal/library"
)

// The catalog's and the library's types, by the names the views use.
type (
	Show      = catalog.Show
	Detail    = catalog.Detail
	Season    = catalog.Season
	HotBoard  = catalog.HotBoard
	RankPage  = catalog.RankPage
	FilterRow = catalog.FilterRow
	Actor     = catalog.Actor
	Person    = catalog.Person
	Record    = library.Record
)

var errText = catalog.ErrText
