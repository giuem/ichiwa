package library

import (
	"testing"

	"hongguo/internal/catalog"
)

// Following a drama and following it again leaves it unfollowed, and the
// latest followed comes first.
func TestFollow(t *testing.T) {
	l := Load(t.TempDir())
	a, b := catalog.Show{ID: "a"}, catalog.Show{ID: "b"}
	l.ToggleFollow(a)
	l.ToggleFollow(b)
	if got := l.Followed(); len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("Followed = %v", got)
	}
	l.ToggleFollow(a)
	if l.Following("a") || len(l.Followed()) != 1 {
		t.Fatal("following again should stop following")
	}
	again := Load(l.path[:len(l.path)-len("/library.json")])
	if !again.Following("b") {
		t.Fatal("follows are not saved")
	}
}
