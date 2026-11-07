package catalog

import (
	"strings"
	"time"

	"github.com/beilunyang/openhongguo/sdk/model"
)

// Actor is a row of the actors' ranking.
type Actor struct {
	ID      string // the account the profile and works are read by
	Name    string
	Avatar  string
	Score   string   // 1060万人气
	Details []string // 54部作品, 双周追剧538万人
	Intro   string   // the titles of some of the works
}

// Person is an actor's profile.
type Person struct {
	ID        string
	Name      string
	Desc      string
	Avatar    string
	Tags      []string // 女演员
	Followers string
	Likes     string
}

// Person loads an actor's profile.
func (b *Backend) Person(id string) (*Person, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	p, err := b.client.Person(ctx, id)
	if err != nil {
		return nil, err
	}
	out := &Person{ID: p.UserID, Name: p.Name, Desc: p.Description, Avatar: p.AvatarURL}
	out.Tags = append(out.Tags, p.Tags...)
	out.Tags = append(out.Tags, p.Labels...)
	if p.Followers > 0 {
		out.Followers = heat(p.Followers)
	}
	if p.Likes > 0 {
		out.Likes = heat(p.Likes)
	}
	return out, nil
}

// Works loads a page of the dramas an actor plays in.
func (b *Backend) Works(id, cursor string) ([]Show, string, bool, error) {
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	page, err := b.client.PersonWorks(ctx, id, cursor)
	if err != nil {
		return nil, "", false, err
	}
	out := make([]Show, 0, len(page.Items))
	for _, w := range page.Items {
		if w.SeriesID != "" {
			out = append(out, workShow(w))
		}
	}
	return out, page.Cursor, page.HasMore && page.Cursor != "", nil
}

// workShow is a work as the grids show it, tagged with its first genre.
func workShow(w model.Work) Show {
	s := Show{ID: w.SeriesID, Title: w.Title, Desc: w.Description, Cover: w.CoverURL, Episodes: w.EpisodeCount}
	for _, t := range w.Tags {
		if !strings.HasSuffix(t, "集") { // 81集 repeats the badge
			s.Tag = t
			break
		}
	}
	if s.Tag == "" && w.Plays > 0 {
		s.Tag = heat(w.Plays) + "播放"
	}
	return s
}

// rankRows splits a ranking's rows into dramas and actors, leaving out the
// rows the server inserts.
func rankRows(items []model.RankItem) ([]Show, []Actor) {
	var shows []Show
	var actors []Actor
	for _, it := range items {
		switch {
		case it.Promoted:
		case it.SeriesID != "":
			tag := it.Score
			if tag == "" && it.Votes > 0 {
				tag = heat(it.Votes)
			}
			shows = append(shows, Show{ID: it.SeriesID, Title: it.Title, Cover: it.CoverURL, Episodes: it.EpisodeCount, Tag: tag})
		case it.UserID != "":
			actors = append(actors, Actor{ID: it.UserID, Name: it.Actor, Avatar: it.AvatarURL, Score: it.Score, Details: it.Details, Intro: it.Intro})
		}
	}
	return shows, actors
}
