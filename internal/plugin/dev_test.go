package plugin

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWatchDirReloads(t *testing.T) {
	js := func(title string) string {
		return strings.Replace(stubJS, `return { items: [{ id: "x", title: "x" }] }`, `return { items: [{ id: "x", title: "`+title+`" }] }`, 1)
	}
	dir := writeDir(t, map[string]string{"plugin.json": goodManifest, "index.js": js("一")})
	p, err := Open(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reloads := make(chan error, 4)
	go p.WatchDir(ctx, dir, 20*time.Millisecond, func(err error) { reloads <- err })
	title := func() string {
		nav, err := p.Navigation(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		return nav[0].Title
	}
	if title() != "一" {
		t.Fatal("first load")
	}
	write := func(body string) error {
		time.Sleep(30 * time.Millisecond) // a new modification time
		os.WriteFile(filepath.Join(dir, "index.js"), []byte(body), 0o644)
		select {
		case err := <-reloads:
			return err
		case <-time.After(5 * time.Second):
			t.Fatal("no reload")
		}
		return nil
	}
	if err := write(js("二")); err != nil || title() != "二" {
		t.Fatalf("reload: %v, title %q", err, title())
	}
	// Broken code keeps the last good version.
	if err := write("export function navigation( {"); err == nil || title() != "二" {
		t.Fatalf("broken reload: %v, title %q", err, title())
	}
	if !strings.Contains(p.Logs().Text(), "重新加载失败") {
		t.Errorf("log:\n%s", p.Logs().Text())
	}
}
