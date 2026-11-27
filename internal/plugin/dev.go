package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// DevInterval is how often the developer mode looks for changed files.
const DevInterval = time.Second

// fingerprint sums the names and contents of the files of a plugin
// folder, the ones ReadDir reads. A folder is small, and contents tell a
// change that a coarse modification time would miss.
func fingerprint(dir string) string {
	h := sha256.New()
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(h, "err:%s;", p)
			return nil
		}
		if p != dir && skipEntry(d.Name()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		fmt.Fprintf(h, "%s:%d:%v;", p, info.Size(), info.Mode().Type())
		if info.Mode().IsRegular() && info.Size() <= MaxUnpackedBytes {
			if f, err := os.Open(p); err == nil {
				_, _ = io.Copy(h, f)
				f.Close()
			}
		}
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// WatchFolder calls changed, on a goroutine of its own, whenever the
// files of the plugin folder dir change from what they are when it is
// called, until ctx ends, checking every interval (DevInterval when 0).
// The developer mode watches a folder this way while its plugin does not
// load, to load it once it is fixed.
func WatchFolder(ctx context.Context, dir string, interval time.Duration, changed func()) {
	last := fingerprint(dir)
	go watchFolder(ctx, dir, last, interval, changed)
}

func watchFolder(ctx context.Context, dir, last string, interval time.Duration, changed func()) {
	if interval <= 0 {
		interval = DevInterval
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if now := fingerprint(dir); now != last {
			last = now
			changed()
		}
	}
}

// WatchDir is the developer mode (§13): it reloads p from the folder dir
// whenever a file in it changes, until ctx ends, checking every interval
// (DevInterval when 0). A failed reload keeps the code that ran before
// and goes to the log. onReload, when set, is called after each attempt
// with its error.
func (p *Plugin) WatchDir(ctx context.Context, dir string, interval time.Duration, onReload func(error)) {
	watchFolder(ctx, dir, fingerprint(dir), interval, func() {
		err := p.ReloadDir(dir)
		if onReload != nil {
			onReload(err)
		}
	})
}

// ReloadDir reloads p from the folder dir, keeping the code that ran
// before when the folder does not load, and notes either in the log.
func (p *Plugin) ReloadDir(dir string) error {
	pkg, err := ReadDir(dir, p.opts.AppVersion)
	if err == nil {
		err = p.Reload(pkg)
	}
	if err != nil {
		p.logs.Hostf("重新加载失败，仍在用之前的代码：%v", err)
	} else {
		p.logs.Hostf("已重新加载 %s %s", pkg.Manifest.Name, pkg.Manifest.Version)
	}
	return err
}
