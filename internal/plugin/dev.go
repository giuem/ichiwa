package plugin

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path/filepath"
	"time"
)

// DevInterval is how often the developer mode looks for changed files.
const DevInterval = time.Second

// fingerprint sums the names, sizes and times of the files of a plugin
// folder, the ones ReadDir reads, so that a change shows without reading
// them.
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
		fmt.Fprintf(h, "%s:%d:%d:%v;", p, info.Size(), info.ModTime().UnixNano(), info.Mode())
		return nil
	})
	return fmt.Sprintf("%x", h.Sum(nil))
}

// WatchDir is the developer mode (§13): it reloads p from the folder dir
// whenever a file in it changes, until ctx ends, checking every interval
// (DevInterval when 0). A failed reload keeps the code that ran before
// and goes to the log. onReload, when set, is called after each attempt
// with its error.
func (p *Plugin) WatchDir(ctx context.Context, dir string, interval time.Duration, onReload func(error)) {
	if interval <= 0 {
		interval = DevInterval
	}
	last := fingerprint(dir)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		now := fingerprint(dir)
		if now == last {
			continue
		}
		last = now
		pkg, err := ReadDir(dir, p.opts.AppVersion)
		if err == nil {
			err = p.Reload(pkg)
		}
		if err != nil {
			p.logs.Hostf("重新加载失败，仍在用之前的代码：%v", err)
		} else {
			p.logs.Hostf("文件有变化，已重新加载 %s %s", pkg.Manifest.Name, pkg.Manifest.Version)
		}
		if onReload != nil {
			onReload(err)
		}
	}
}
