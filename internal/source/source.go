// Package source defines the protocol models, capabilities, errors, and Source
// interface shared between the host and all video sources.
package source

import (
	"context"
	"slices"
)

// Source is a video source. Methods may be called concurrently and must return
// promptly when ctx is canceled.
type Source interface {
	Info() Info

	Navigation(ctx context.Context) ([]NavItem, error)
	Collection(ctx context.Context, r CollectionRequest) (*CollectionPage, error)
	Media(ctx context.Context, id string) (*Media, error)
	Play(ctx context.Context, r PlayRequest) (*Playback, error)

	// Optional capabilities. The host does not call these when Info().Has
	// returns false; implementations without the capability return ErrUnsupported.
	Search(ctx context.Context, r SearchRequest) (*CollectionPage, error)
	SearchHome(ctx context.Context) (*CollectionPage, error)
	Suggest(ctx context.Context, query string) ([]string, error)
	Danmaku(ctx context.Context, r DanmakuRequest) (*DanmakuPage, error)
	Refresh(ctx context.Context, ids []string) ([]Card, error)
}

// Capability names an optional feature a source may support.
type Capability string

const (
	CapSearch        Capability = "search"
	CapSearchSuggest Capability = "search.suggest"
	CapSearchHome    Capability = "search.home"
	CapDanmaku       Capability = "danmaku"
	CapRefresh       Capability = "refresh"
)

// Info describes a source: its identity, display metadata, and capabilities.
type Info struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description,omitempty"`
	Version      string            `json:"version,omitempty"`
	Icon         []byte            `json:"-"`
	ImageHeaders map[string]string `json:"-"`
	// Network lists the hosts a plugin declared (permissions.network):
	// its images and media must be on them, and the host fetches them
	// through the network guard. Nil for a built-in source, which the
	// host trusts.
	Network      []string     `json:"-"`
	Capabilities []Capability `json:"capabilities,omitempty"`
}

// Has reports whether the source supports capability c.
// Capabilities that depend on CapSearch (search.suggest, search.home) also
// require CapSearch to be present.
func (i Info) Has(c Capability) bool {
	switch c {
	case CapSearchSuggest, CapSearchHome:
		if !slices.Contains(i.Capabilities, CapSearch) {
			return false
		}
	}
	return slices.Contains(i.Capabilities, c)
}

// Ref uniquely identifies an item within a source.
type Ref struct {
	Source string `json:"source"`
	ID     string `json:"id"`
}

// CardKind tells what opening a Card navigates to.
type CardKind string

const (
	CardMedia      CardKind = "media"
	CardCollection CardKind = "collection"
)

// Shape describes the aspect shape of a card or header image.
type Shape string

const (
	ShapePortrait  Shape = "portrait"
	ShapeLandscape Shape = "landscape"
	ShapeSquare    Shape = "square"
	ShapeCircle    Shape = "circle"
)

// Card is an item in a list, grid, ranking, or section.
type Card struct {
	Kind     CardKind `json:"kind"`
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Image    string   `json:"image,omitempty"`
	Shape    Shape    `json:"shape,omitempty"`
	Badges   []string `json:"badges,omitempty"`
	Note     string   `json:"note,omitempty"`
}

// Icon is a host-defined sidebar icon name for a navigation item.
type Icon string

const (
	IconHome     Icon = "home"
	IconFire     Icon = "fire"
	IconStar     Icon = "star"
	IconSparkles Icon = "sparkles"
	IconTrophy   Icon = "trophy"
	IconFilter   Icon = "filter"
	IconFilm     Icon = "film"
	IconTV       Icon = "tv"
	IconDrama    Icon = "drama"
	IconAnime    Icon = "anime"
	IconMusic    Icon = "music"
	IconNews     Icon = "news"
	IconSport    Icon = "sport"
	IconKids     Icon = "kids"
	IconCalendar Icon = "calendar"
	IconGrid     Icon = "grid"
)

// NavItem is an entry in a source's navigation list, each opening a collection.
type NavItem struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  Icon   `json:"icon,omitempty"`
}

// Layout controls how a CollectionPage's items are arranged.
type Layout string

const (
	LayoutGrid   Layout = "grid"
	LayoutList   Layout = "list"
	LayoutRanked Layout = "ranked"
)

// SectionLayout controls how a Section's items are arranged.
type SectionLayout string

const (
	SectionRow    SectionLayout = "row"
	SectionGrid   SectionLayout = "grid"
	SectionRanked SectionLayout = "ranked"
)

// CollectionRequest asks for one page of a collection.
type CollectionRequest struct {
	ID      string            `json:"id"`
	Filters map[string]string `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
}

// CollectionPage is one page returned by Collection, Search, or SearchHome.
type CollectionPage struct {
	Title    string      `json:"title,omitempty"`
	Header   *Header     `json:"header,omitempty"`
	Filters  []FilterRow `json:"filters,omitempty"`
	Layout   Layout      `json:"layout,omitempty"`
	Sections []Section   `json:"sections,omitempty"`
	Items    []Card      `json:"items,omitempty"`
	Cursor   string      `json:"cursor,omitempty"`
}

// Header introduces a collection itself, such as an actor profile page.
type Header struct {
	Title       string `json:"title"`
	Image       string `json:"image,omitempty"`
	Shape       Shape  `json:"shape,omitempty"`
	Description string `json:"description,omitempty"`
	Meta        []Meta `json:"meta,omitempty"`
}

// Meta is a short fact shown in a header or media detail.
type Meta struct {
	Label string `json:"label,omitempty"`
	Value string `json:"value"`
}

// FilterOption is a single choice inside a FilterRow.
type FilterOption struct {
	Value string `json:"value"`
	Title string `json:"title"`
}

// FilterRow is one row of filter options for a collection.
type FilterRow struct {
	Key     string         `json:"key"`
	Title   string         `json:"title"`
	Options []FilterOption `json:"options"`
	Value   string         `json:"value"`
}

// Section is a named group of cards shown ahead of items or in media details.
type Section struct {
	Title      string        `json:"title"`
	Layout     SectionLayout `json:"layout,omitempty"`
	Items      []Card        `json:"items,omitempty"`
	Collection string        `json:"collection,omitempty"`
	More       string        `json:"more,omitempty"`
}

// Aspect is the picture orientation of a media or episode.
type Aspect string

const (
	AspectPortrait  Aspect = "portrait"
	AspectLandscape Aspect = "landscape"
)

// Lock explains why an episode cannot be played.
type Lock string

const (
	LockNone        Lock = ""
	LockLogin       Lock = "login"
	LockVIP         Lock = "vip"
	LockPaid        Lock = "paid"
	LockUnavailable Lock = "unavailable"
)

// Media is the full detail of a drama, movie, series season, or video.
type Media struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Image       string    `json:"image,omitempty"`
	Backdrop    string    `json:"backdrop,omitempty"`
	Description string    `json:"description,omitempty"`
	Badges      []string  `json:"badges,omitempty"`
	Meta        []Meta    `json:"meta,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	People      []Card    `json:"people,omitempty"`
	Aspect      Aspect    `json:"aspect,omitempty"`
	Seasons     []Season  `json:"seasons,omitempty"`
	Episodes    []Episode `json:"episodes"`
	Sections    []Section `json:"sections,omitempty"`
}

// Season is one season of a multi-season work.
type Season struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Name      string `json:"name,omitempty"`
	Image     string `json:"image,omitempty"`
	Current   bool   `json:"current,omitempty"`
	Available *bool  `json:"available,omitempty"`
	Note      string `json:"note,omitempty"`
}

// IsAvailable reports whether the season is online. When Available is omitted
// (nil), it defaults to true per protocol specification.
func (s Season) IsAvailable() bool {
	return s.Available == nil || *s.Available
}

// Bool returns a pointer to v, for optional boolean protocol fields.
func Bool(v bool) *bool { return &v }

// Episode is one playable unit inside a Media.
type Episode struct {
	ID       string   `json:"id"`
	Title    string   `json:"title,omitempty"`
	Duration float64  `json:"duration,omitempty"`
	Image    string   `json:"image,omitempty"`
	Badges   []string `json:"badges,omitempty"`
	Aspect   Aspect   `json:"aspect,omitempty"`
	Lock     Lock     `json:"lock,omitempty"`
}

// SearchRequest asks for one page of search results.
type SearchRequest struct {
	Query   string            `json:"query"`
	Filters map[string]string `json:"filters,omitempty"`
	Cursor  string            `json:"cursor,omitempty"`
}

// DanmakuRequest asks for one page of an episode's danmaku comments.
type DanmakuRequest struct {
	MediaID   string `json:"mediaId"`
	EpisodeID string `json:"episodeId"`
	Cursor    string `json:"cursor,omitempty"`
}

// Danmaku is one comment flying over an episode at Time seconds.
type Danmaku struct {
	ID   string  `json:"id,omitempty"`
	Text string  `json:"text"`
	Time float64 `json:"time"`
}

// DanmakuPage is one page of danmaku sorted by Time.
type DanmakuPage struct {
	Items  []Danmaku `json:"items"`
	Cursor string    `json:"cursor,omitempty"`
}

// Container is the media container format of a Stream.
type Container string

const (
	ContainerMP4 Container = "mp4"
	ContainerHLS Container = "hls"
)

// EncryptionScheme names a stream decryption scheme supported by the host.
type EncryptionScheme string

const (
	EncryptionCENC EncryptionScheme = "cenc"
)

// PlayPrefer conveys the host's device capabilities to Play.
type PlayPrefer struct {
	MaxHeight int  `json:"maxHeight"`
	HEVC      bool `json:"hevc"`
}

// PlayRequest asks for playable streams of an episode.
type PlayRequest struct {
	MediaID   string     `json:"mediaId"`
	EpisodeID string     `json:"episodeId"`
	Prefer    PlayPrefer `json:"prefer"`
}

// Playback is the set of streams returned by Play.
type Playback struct {
	Streams   []Stream `json:"streams"`
	ExpiresAt int64    `json:"expiresAt,omitempty"`
}

// Stream is one playable representation of an episode.
type Stream struct {
	URL        string            `json:"url"`
	BackupURLs []string          `json:"backupUrls,omitempty"`
	Container  Container         `json:"container"`
	Headers    map[string]string `json:"headers,omitempty"`
	Codec      string            `json:"codec,omitempty"`
	Width      int               `json:"width,omitempty"`
	Height     int               `json:"height,omitempty"`
	Bitrate    int64             `json:"bitrate,omitempty"`
	Encryption *Encryption       `json:"encryption,omitempty"`
}

// Encryption describes how a Stream is encrypted.
type Encryption struct {
	Scheme EncryptionScheme `json:"scheme"`
	Key    string           `json:"key"`
}
