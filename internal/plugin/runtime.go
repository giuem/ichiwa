// Package plugin runs source plugins (docs/plugin-design.md): it reads and
// checks a plugin package, compiles it with moejs, runs every call in a
// fresh sandboxed runtime with the host API, and adapts the plugin to
// source.Source.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/eventloop"
	"github.com/MoeForge/moejs/webapi"

	"github.com/giuem/ichiwa/internal/netguard"
	"github.com/giuem/ichiwa/internal/source"
)

// Limits of one call (§9).
const (
	MemoryLimit     = 64 << 20
	CallTimeout     = 15 * time.Second
	LongCallTimeout = 30 * time.Second // media and play
	MaxConcurrent   = 4
)

// Options configures how plugins run.
type Options struct {
	// AppVersion is this app's version, for minAppVersion and
	// host.device().
	AppVersion string
	// DataDir keeps each plugin's host.storage, in DataDir/<id>/; ""
	// keeps it in memory.
	DataDir string
	// Guard checks the addresses fetches connect to; nil uses the
	// system resolver and refuses this machine and private networks.
	Guard *netguard.Guard
	// Timeout and LongTimeout replace CallTimeout and LongCallTimeout
	// when set, for tests.
	Timeout, LongTimeout time.Duration
}

// loaded is one compiled version of a plugin; a reload swaps it whole.
type loaded struct {
	pkg *Package
	mod *moejs.Module
}

// Plugin is a loaded plugin. It is safe for concurrent use; at most
// MaxConcurrent calls run at once, the rest wait.
type Plugin struct {
	opts      Options
	cur       atomic.Pointer[loaded]
	slots     chan struct{}
	logs      *Logs
	storage   *Storage
	cache     *Cache
	jar       *cookiejar.Jar
	transport http.RoundTripper
}

// New compiles pkg and returns the plugin, which runs nothing until
// called.
func New(pkg *Package, opts Options) (*Plugin, error) {
	mod, err := compile(pkg)
	if err != nil {
		return nil, err
	}
	guard := opts.Guard
	if guard == nil {
		guard = &netguard.Guard{}
	}
	storagePath := ""
	if opts.DataDir != "" {
		storagePath = filepath.Join(opts.DataDir, pkg.Manifest.ID, "storage.json")
	}
	p := &Plugin{
		opts:      opts,
		slots:     make(chan struct{}, MaxConcurrent),
		logs:      &Logs{},
		storage:   OpenStorage(storagePath),
		cache:     NewCache(),
		jar:       newJar(),
		transport: guard.Transport(),
	}
	p.cur.Store(&loaded{pkg: pkg, mod: mod})
	return p, nil
}

// Open reads, checks and compiles the plugin at path, a folder or a zip.
func Open(path string, opts Options) (*Plugin, error) {
	pkg, err := ReadPath(path, opts.AppVersion)
	if err != nil {
		return nil, err
	}
	return New(pkg, opts)
}

// Check reads and compiles the plugin at path without running it, as an
// install does (§13).
func Check(path, appVersion string) (*Package, error) {
	pkg, err := ReadPath(path, appVersion)
	if err != nil {
		return nil, err
	}
	if _, err := compile(pkg); err != nil {
		return nil, err
	}
	return pkg, nil
}

// Reload replaces the plugin's code with pkg, keeping its storage, cache,
// cookies and log. The id must stay the same.
func (p *Plugin) Reload(pkg *Package) error {
	if old := p.Manifest(); old.ID != pkg.Manifest.ID {
		return fmt.Errorf("插件 id 从 %s 变成了 %s，请重新加载", old.ID, pkg.Manifest.ID)
	}
	mod, err := compile(pkg)
	if err != nil {
		return err
	}
	p.cur.Store(&loaded{pkg: pkg, mod: mod})
	return nil
}

// Manifest returns the manifest of the code loaded now.
func (p *Plugin) Manifest() *Manifest { return p.cur.Load().pkg.Manifest }

// Package returns the package loaded now.
func (p *Plugin) Package() *Package { return p.cur.Load().pkg }

// Logs returns the plugin's log.
func (p *Plugin) Logs() *Logs { return p.logs }

func (p *Plugin) timeout(fn string) time.Duration {
	long := fn == "media" || fn == "play"
	switch {
	case long && p.opts.LongTimeout > 0:
		return p.opts.LongTimeout
	case long:
		return LongCallTimeout
	case p.opts.Timeout > 0:
		return p.opts.Timeout
	}
	return CallTimeout
}

// errNoResult is a call whose promise never settled.
var errNoResult = errors.New("函数没有返回结果")

// Call runs export fn with arg (marshaled to JSON; nil passes no
// argument) in a fresh runtime, and returns the JSON of what it returned
// or resolved to, at most source.MaxResponseBytes. A failure is a
// *source.Error (§10), or ctx's error when the caller canceled.
func (p *Plugin) Call(ctx context.Context, fn string, arg any) (json.RawMessage, error) {
	var argJSON []byte
	if arg != nil {
		var err error
		if argJSON, err = json.Marshal(arg); err != nil {
			return nil, source.NewError(source.ErrPlugin, "", err)
		}
	}
	select {
	case p.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-p.slots }()

	callCtx, cancel := context.WithTimeout(ctx, p.timeout(fn))
	defer cancel()
	start := time.Now()
	out, err := p.run(callCtx, fn, argJSON)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err() // the caller canceled: not an error (§9)
		}
		var se *source.Error
		if errors.As(err, &se) && se.Code != source.ErrPlugin {
			p.logs.Hostf("%s 失败（%s）：%s", fn, se.Code, se.Error())
		} else {
			p.logs.Hostf("%s 失败：%v", fn, describeErr(err))
		}
		return nil, err
	}
	p.logs.Hostf("%s 用时 %d ms，返回 %d 字节", fn, time.Since(start).Milliseconds(), len(out))
	return out, nil
}

func describeErr(err error) string {
	var se *source.Error
	if errors.As(err, &se) && se.Cause != nil {
		return se.Error() + "：" + se.Cause.Error()
	}
	return err.Error()
}

// thrown is what the helper's invoke tells of a rejection.
type thrown struct {
	Host       bool    `json:"host"`
	Code       string  `json:"code"`
	Message    string  `json:"message"`
	RetryAfter float64 `json:"retryAfter"`
	Text       string  `json:"text"`
	Fetch      bool    `json:"fetch"` // fetch rejected with it
}

func (p *Plugin) run(ctx context.Context, fn string, argJSON []byte) (json.RawMessage, error) {
	l := p.cur.Load()
	rt := moejs.NewRuntime(moejs.Options{
		MemoryLimit:        MemoryLimit,
		DisableDynamicCode: true,
		Locale:             "zh-CN",
	})
	loop, err := eventloop.New(rt, eventloop.Options{})
	if err != nil {
		return nil, source.NewError(source.ErrPlugin, "", err)
	}
	defer loop.Terminate()
	var netFailed atomic.Bool
	hosts := l.pkg.Manifest.Hosts()
	api, err := webapi.Install(loop, webapi.Options{
		Client:  p.client(hosts, &netFailed),
		Allow:   allowRequest(hosts, p.logs),
		Context: ctx,
	})
	if err != nil {
		return nil, source.NewError(source.ErrPlugin, "", err)
	}
	defer api.Close()
	stop := context.AfterFunc(ctx, func() { loop.Interrupt(context.Cause(ctx)) })
	defer stop()

	cs := &callState{p: p, rt: rt}
	var (
		result  json.RawMessage
		failure *thrown
		settled bool
		decErr  error
		once    sync.Once
	)
	err = loop.Run(func(rt *moejs.Runtime) error {
		helperFn, err := rt.RunScript(helperScript)
		if err != nil {
			return err
		}
		nat, err := rt.FromGo(cs.natives())
		if err != nil {
			return err
		}
		helper, err := rt.CallFunction(helperFn, moejs.Undefined(), nat)
		if err != nil {
			return err
		}
		for _, g := range []string{"host", "console"} {
			v, err := rt.Get(helper, g)
			if err != nil {
				return err
			}
			if err := rt.SetGlobal(g, v); err != nil {
				return err
			}
		}
		invoke, err := rt.Get(helper, "invoke")
		if err != nil {
			return err
		}
		if err := rt.Load(l.mod); err != nil {
			return err
		}
		f, ok := rt.Export(fn)
		if !ok || !f.IsObject() || !f.AsObject().IsCallable() {
			return fmt.Errorf("没有导出函数 %s", fn)
		}
		arg := moejs.Undefined()
		if argJSON != nil {
			if arg, err = rt.ParseJSON(argJSON); err != nil {
				return err
			}
		}
		resolve := rt.Function("resolve", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			once.Do(func() {
				settled = true
				decErr = rt.UnmarshalWith(moejs.Arg(args, 0), &result, moejs.DecodeOptions{MaxBytes: source.MaxResponseBytes})
				loop.StopNoWait()
			})
			return moejs.Undefined(), nil
		})
		reject := rt.Function("reject", 1, func(_ *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			once.Do(func() {
				settled = true
				var t thrown
				if err := rt.Unmarshal(moejs.Arg(args, 0), &t); err != nil {
					t = thrown{Text: "无法读取的异常"}
				}
				failure = &t
				loop.StopNoWait()
			})
			return moejs.Undefined(), nil
		})
		_, err = rt.CallFunction(invoke, moejs.Undefined(), f, arg, resolve, reject)
		return err
	})
	switch {
	case ctx.Err() != nil:
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return nil, source.NewError(source.ErrPlugin, "片源响应超时", context.DeadlineExceeded)
		}
		return nil, ctx.Err()
	case err != nil && errors.Is(err, moejs.ErrMemoryLimit):
		return nil, source.NewError(source.ErrPlugin, "片源占用内存过多", err)
	case err != nil:
		p.logs.add("error", errorText(rt, err))
		return nil, source.NewError(source.ErrPlugin, "", err)
	case failure != nil:
		return nil, p.failure(failure, netFailed.Load())
	case !settled:
		return nil, source.NewError(source.ErrPlugin, "", errNoResult)
	case decErr != nil:
		if errors.Is(decErr, moejs.ErrTooLarge) {
			return nil, source.NewError(source.ErrPlugin, "片源返回数据过大", decErr)
		}
		return nil, source.NewError(source.ErrPlugin, "片源返回格式错误", decErr)
	}
	return result, nil
}

func errorText(rt *moejs.Runtime, err error) string {
	var exc *moejs.Exception
	if errors.As(err, &exc) {
		if st := rt.StackTrace(exc); st != "" {
			return st
		}
	}
	return err.Error()
}

// failure turns what the plugin threw into a *source.Error (§10): a
// host.error keeps its code, anything else is the plugin's error, or a
// network error when it is what a fetch rejected with, uncaught, and a
// fetch of the call failed on the network (not refused by the sandbox).
func (p *Plugin) failure(t *thrown, netFailed bool) error {
	if t.Host {
		code := source.ErrorCode(strings.TrimSpace(t.Code))
		e := source.NewError(code, t.Message, nil)
		if t.RetryAfter > 0 && t.RetryAfter < 7*24*3600 {
			e.RetryAfter = time.Duration(t.RetryAfter * float64(time.Second))
		}
		return e
	}
	p.logs.add("error", t.Text)
	if t.Fetch && netFailed {
		return source.NewError(source.ErrNetwork, "", errors.New(t.Text))
	}
	return source.NewError(source.ErrPlugin, "", errors.New(t.Text))
}

// UseStorage replaces the plugin's host.storage, for the command line
// tool, which keeps it in a file of the developer's choice.
func (p *Plugin) UseStorage(s *Storage) { p.storage = s }
