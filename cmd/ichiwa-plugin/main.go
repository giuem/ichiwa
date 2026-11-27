// Command ichiwa-plugin helps write source plugins for 一集 (Ichiwa):
//
//	ichiwa-plugin check <folder|zip>             check the manifest, the files and the exports
//	ichiwa-plugin call <folder|zip> <function> [JSON]
//	                                             call a function in the same sandbox as the app,
//	                                             print what the app gets and the log
//	ichiwa-plugin pack <folder> [-o plugin.zip]  check and pack the folder
//
// See docs/plugin-development.md.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/giuem/ichiwa/internal/plugin"
	"github.com/giuem/ichiwa/internal/source"
)

const usage = `用法：
  ichiwa-plugin check <目录或 zip>
      检查清单、文件和导出的函数，不运行代码。
  ichiwa-plugin call [-raw] [-storage 文件] <目录或 zip> <函数> ['<JSON 参数>']
      在和一集相同的沙箱里调用一个函数，打印一集收到的结果和插件日志。
      -raw 打印插件返回的原始 JSON，不经过一集的检查和清洗。
      -storage 把 host.storage 存到这个文件，多次调用之间保留；不给就只在内存里。
  ichiwa-plugin pack [-o plugin.zip] <目录>
      检查并打包成 plugin.zip。

函数：navigation collection media play search searchHome suggest danmaku refresh
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "check":
		err = check(os.Args[2:], os.Stdout)
	case "call":
		err = call(os.Args[2:], os.Stdout, os.Stderr)
	case "pack":
		err = pack(os.Args[2:], os.Stdout)
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

// appVersion is empty: the tool checks no minAppVersion against itself.
const appVersion = ""

func check(args []string, out io.Writer) error {
	if len(args) != 1 {
		return errors.New("用法：ichiwa-plugin check <目录或 zip>")
	}
	pkg, err := plugin.Check(args[0], appVersion)
	if err != nil {
		return err
	}
	m := pkg.Manifest
	fmt.Fprintf(out, "%s %s（%s）\n", m.Name, m.Version, m.ID)
	if m.Description != "" {
		fmt.Fprintln(out, m.Description)
	}
	fmt.Fprintf(out, "函数：%s\n", strings.Join(m.Functions(), "、"))
	fmt.Fprintf(out, "能访问的主机：%s\n", strings.Join(orNone(m.Network), "、"))
	fmt.Fprintln(out, "检查通过")
	return nil
}

func orNone(s []string) []string {
	if len(s) == 0 {
		return []string{"（无）"}
	}
	return s
}

func call(args []string, out, logOut io.Writer) error {
	fs := flag.NewFlagSet("call", flag.ContinueOnError)
	fs.SetOutput(logOut)
	raw := fs.Bool("raw", false, "")
	storage := fs.String("storage", "", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	rest := fs.Args()
	if len(rest) < 2 || len(rest) > 3 {
		return errors.New("用法：ichiwa-plugin call <目录或 zip> <函数> ['<JSON 参数>']")
	}
	opts := plugin.Options{AppVersion: appVersion}
	var dataDir string
	if *storage != "" {
		dataDir = filepath.Dir(*storage)
	}
	p, err := plugin.Open(rest[0], opts)
	if err != nil {
		return err
	}
	if dataDir != "" {
		p.UseStorage(plugin.OpenStorage(*storage))
	}
	fn := rest[1]
	var arg json.RawMessage
	if len(rest) == 3 {
		arg = json.RawMessage(rest[2])
		if !json.Valid(arg) {
			return errors.New("参数不是有效的 JSON")
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	stopLogs := p.Logs().Watch(func(e plugin.LogEntry) { fmt.Fprintln(logOut, e.String()) })
	defer stopLogs()

	var result any
	if *raw {
		var a any
		if arg != nil {
			a = arg
		}
		result, err = p.Call(ctx, fn, a)
	} else {
		result, err = callSource(ctx, p, fn, arg)
	}
	if err != nil {
		var se *source.Error
		if errors.As(err, &se) {
			msg := fmt.Sprintf("%s：%s", se.Code, se.Error())
			if se.RetryAfter > 0 {
				msg += fmt.Sprintf("（%v 后重试）", se.RetryAfter)
			}
			if !isNil(result) {
				// A page and an error: the list failed (§6).
				printJSON(out, result)
				return fmt.Errorf("列表部分失败 %s", msg)
			}
			return errors.New(msg)
		}
		return err
	}
	return printJSON(out, result)
}

func isNil(v any) bool {
	rv := reflect.ValueOf(v)
	return !rv.IsValid() || rv.Kind() == reflect.Pointer && rv.IsNil() || rv.Kind() == reflect.Slice && rv.IsNil()
}

func printJSON(out io.Writer, v any) error {
	if r, ok := v.(json.RawMessage); ok {
		var a any
		if json.Unmarshal(r, &a) == nil {
			v = a
		}
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// callSource calls fn through the source.Source adapter, so the output
// is what the app gets: checked and cleaned (§6).
func callSource(ctx context.Context, p *plugin.Plugin, fn string, arg json.RawMessage) (any, error) {
	decode := func(v any) error {
		if arg == nil {
			return nil
		}
		return json.Unmarshal(arg, v)
	}
	switch fn {
	case "navigation":
		return p.Navigation(ctx)
	case "collection":
		var r source.CollectionRequest
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Collection(ctx, r)
	case "media":
		var r struct {
			ID string `json:"id"`
		}
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Media(ctx, r.ID)
	case "play":
		r := source.PlayRequest{Prefer: source.PlayPrefer{MaxHeight: 1080, HEVC: true}}
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Play(ctx, r)
	case "search":
		var r source.SearchRequest
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Search(ctx, r)
	case "searchHome":
		return p.SearchHome(ctx)
	case "suggest":
		var r struct {
			Query string `json:"query"`
		}
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Suggest(ctx, r.Query)
	case "danmaku":
		var r source.DanmakuRequest
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Danmaku(ctx, r)
	case "refresh":
		var r struct {
			IDs []string `json:"ids"`
		}
		if err := decode(&r); err != nil {
			return nil, err
		}
		return p.Refresh(ctx, r.IDs)
	}
	return nil, fmt.Errorf("不认识的函数 %q", fn)
}

func pack(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("pack", flag.ContinueOnError)
	output := fs.String("o", "plugin.zip", "")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("用法：ichiwa-plugin pack [-o plugin.zip] <目录>")
	}
	data, err := plugin.Pack(fs.Arg(0), appVersion)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*output, data, 0o644); err != nil {
		return err
	}
	fmt.Fprintf(out, "已打包 %s（%d 字节）\n", *output, len(data))
	return nil
}
