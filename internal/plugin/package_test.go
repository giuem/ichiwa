package plugin

import (
	"archive/zip"
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const goodManifest = `{"id":"zipped","name":"压缩包","kind":"source","version":"1.0.0","apiVersion":1,"main":"index.js"}`

const stubJS = `export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function collection() { return {}; }
export function media() { return {}; }
export function play() { return {}; }
`

type zipEntry struct {
	name string
	body string
	mode os.FileMode
}

func makeZip(t *testing.T, entries ...zipEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e.body))
	}
	zw.Close()
	return buf.Bytes()
}

func goodEntries() []zipEntry {
	return []zipEntry{{name: "plugin.json", body: goodManifest}, {name: "index.js", body: stubJS}}
}

func TestReadZip(t *testing.T) {
	pkg, err := ReadZip(makeZip(t, goodEntries()...), "")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.Manifest.ID != "zipped" || len(pkg.Files) != 2 {
		t.Fatalf("pkg = %+v", pkg)
	}
	if _, err := New(pkg, Options{}); err != nil {
		t.Fatal(err)
	}
}

func TestReadZipRefusesBadPaths(t *testing.T) {
	for _, name := range []string{
		"../evil.js",
		"lib/../../evil.js",
		"/abs.js",
		`lib\evil.js`,
		"lib/./x.js",
		"bad\x01name.js",
		"c:/x.js",
	} {
		_, err := ReadZip(makeZip(t, append(goodEntries(), zipEntry{name: name, body: "x"})...), "")
		if err == nil {
			t.Errorf("%q accepted", name)
		}
	}
}

func TestReadZipRefusesSymlinksAndCaseClashes(t *testing.T) {
	if _, err := ReadZip(makeZip(t, append(goodEntries(), zipEntry{name: "link.js", body: "/etc/passwd", mode: os.ModeSymlink | 0o777})...), ""); err == nil {
		t.Error("symbolic link accepted")
	}
	if _, err := ReadZip(makeZip(t, append(goodEntries(), zipEntry{name: "INDEX.js", body: "x"})...), ""); err == nil {
		t.Error("names differing in case accepted")
	}
	if _, err := ReadZip(makeZip(t, append(goodEntries(), zipEntry{name: "Index.js/x.js", body: "x"})...), ""); err == nil {
		t.Error("a folder named like a file accepted")
	}
}

func TestReadZipSizeLimits(t *testing.T) {
	if _, err := ReadZip(make([]byte, MaxZipBytes+1), ""); err == nil {
		t.Error("oversized zip accepted")
	}
	// A zip bomb: small packed, more than 32 MiB unpacked.
	big := strings.Repeat("a", 17<<20)
	bomb := makeZip(t, append(goodEntries(), zipEntry{name: "a.bin", body: big}, zipEntry{name: "b.bin", body: big})...)
	if len(bomb) > MaxZipBytes {
		t.Fatalf("test zip is %d bytes", len(bomb))
	}
	if _, err := ReadZip(bomb, ""); err == nil || !strings.Contains(err.Error(), "解压后") {
		t.Errorf("zip bomb: %v", err)
	}
}

func writeDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestReadDirRefusesSymlinks(t *testing.T) {
	dir := writeDir(t, map[string]string{"plugin.json": goodManifest, "index.js": stubJS})
	secret := filepath.Join(t.TempDir(), "secret.js")
	os.WriteFile(secret, []byte("secret"), 0o644)
	if err := os.Symlink(secret, filepath.Join(dir, "lib.js")); err != nil {
		t.Skipf("cannot make symbolic links here: %v", err)
	}
	if _, err := ReadDir(dir, ""); err == nil || !strings.Contains(err.Error(), "符号链接") {
		t.Fatalf("symbolic link: %v", err)
	}
}

func TestReadDirSkipsHiddenAndNodeModules(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"plugin.json":         goodManifest,
		"index.js":            stubJS,
		".git/HEAD":           "ref",
		"node_modules/x/a.js": "x",
		"lib/util.js":         "export const a = 1;",
	})
	pkg, err := ReadDir(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pkg.Files) != 3 {
		t.Fatalf("files = %v", keys(pkg.Files))
	}
}

func keys(m map[string][]byte) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestImports(t *testing.T) {
	dir := writeDir(t, map[string]string{
		"plugin.json": goodManifest,
		"index.js":    `import { x } from "./lib/a.js"; export { navigation, collection, media, play } from "./lib/fns.js"; export const y = x;`,
		"lib/a.js":    `import { z } from "../lib/b.js"; export const x = z;`,
		"lib/b.js":    `export const z = 1;`,
		"lib/fns.js":  stubJS,
	})
	if _, err := Check(dir, ""); err != nil {
		t.Fatalf("relative imports: %v", err)
	}
	for _, bad := range []string{`import "fs";`, `import "/abs.js";`, `import "../outside.js";`, `import "./missing.js";`} {
		d := writeDir(t, map[string]string{"plugin.json": goodManifest, "index.js": bad + "\n" + stubJS})
		if _, err := Check(d, ""); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
	if _, err := Open(filepath.Join("testdata", "hostile", "escape"), Options{}); err == nil {
		t.Error("an import out of the package accepted")
	}
}

func TestMissingExports(t *testing.T) {
	_, err := Open(filepath.Join("testdata", "hostile", "missing"), Options{})
	if err == nil || !strings.Contains(err.Error(), "search") {
		t.Fatalf("a declared capability without its function: %v", err)
	}
	d := writeDir(t, map[string]string{"plugin.json": goodManifest, "index.js": "export function navigation() {}"})
	if _, err := Check(d, ""); err == nil {
		t.Fatal("missing required exports accepted")
	}
}

func TestManifest(t *testing.T) {
	base := map[string]string{
		"id": `"ok"`, "name": `"名字"`, "kind": `"source"`, "version": `"1.0.0"`, "apiVersion": `1`, "main": `"index.js"`,
	}
	build := func(over map[string]string) []byte {
		m := map[string]string{}
		for k, v := range base {
			m[k] = v
		}
		for k, v := range over {
			if v == "" {
				delete(m, k)
			} else {
				m[k] = v
			}
		}
		var parts []string
		for k, v := range m {
			parts = append(parts, `"`+k+`":`+v)
		}
		return []byte("{" + strings.Join(parts, ",") + "}")
	}
	if _, err := ParseManifest(build(nil), ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"a", "a.b", "a_b-c", strings.Repeat("a", 64)} {
		if _, err := ParseManifest(build(map[string]string{"id": `"` + id + `"`}), ""); err != nil {
			t.Errorf("id %q: %v", id, err)
		}
	}
	bad := []map[string]string{
		{"id": `"Bad"`},
		{"id": `"-x"`},
		{"id": `"x."`},
		{"id": `"x-"`},
		{"id": `"x_"`},
		{"id": `"` + strings.Repeat("a", 65) + `"`},
		{"id": ""},
		{"permissions": `{"network":["8.526344"]}`},
		{"permissions": `{"network":["0x08.0x08.0x08.0x08"]}`},
		{"permissions": `{"network":["b\u00fccher.example"]}`},
		{"name": `"` + strings.Repeat("名", 33) + `"`},
		{"kind": `"theme"`},
		{"version": `"1.0"`},
		{"apiVersion": `2`},
		{"main": `"../index.js"`},
		{"main": `"index.ts"`},
		{"minAppVersion": `"2099.101.1"`},
		{"minAppVersion": `"soon"`},
		{"permissions": `{"network":["127.0.0.1"]}`},
		{"permissions": `{"network":["localhost"]}`},
		{"permissions": `{"network":["api.example.com:8080"]}`},
		{"permissions": `{"network":["*.com"]}`},
		{"permissions": `{"network":["*.co.uk"]}`},
		{"permissions": `{"network":["com"]}`},
		{"permissions": `{"network":["*"]}`},
		{"permissions": `{"network":["[::1]"]}`},
		{"images": `{"headers":{"Cookie":"a=1"}}`},
		{"images": `{"headers":{"Authorization":"x"}}`},
	}
	for _, over := range bad {
		if _, err := ParseManifest(build(over), "2026.1201.1"); err == nil {
			t.Errorf("%v accepted", over)
		}
	}
	m, err := ParseManifest(build(map[string]string{
		"permissions":   `{"network":["api.example.com","*.cdn.example.com"]}`,
		"images":        `{"headers":{"Referer":"https://example.com/"}}`,
		"capabilities":  `["search","teleport",3,"danmaku","search"]`,
		"minAppVersion": `"2026.1101.2"`,
		"future":        `{"anything":true}`,
	}), "2026.1201.1")
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Capabilities) != 2 || len(m.Network) != 2 || m.ImageHeaders["Referer"] == "" {
		t.Fatalf("m = %+v", m)
	}
	if !m.Hosts().Allows("img.cdn.example.com") || m.Hosts().Allows("cdn.example.com") || m.Hosts().Allows("example.com") {
		t.Fatal("host matching")
	}
	// A development build checks no minAppVersion.
	if _, err := ParseManifest(build(map[string]string{"minAppVersion": `"2099.101.1"`}), "0.0.0-dev"); err != nil {
		t.Fatal(err)
	}
}

func TestIcon(t *testing.T) {
	icon := func(w, h int) string {
		var buf bytes.Buffer
		png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)))
		return buf.String()
	}
	man := `{"id":"icon","name":"图标","kind":"source","version":"1.0.0","apiVersion":1,"main":"index.js","icon":"icon.png"}`
	if _, err := ReadDir(writeDir(t, map[string]string{"plugin.json": man, "index.js": stubJS, "icon.png": icon(128, 128)}), ""); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{icon(64, 64), icon(200, 128), "not an image"} {
		if _, err := ReadDir(writeDir(t, map[string]string{"plugin.json": man, "index.js": stubJS, "icon.png": bad}), ""); err == nil {
			t.Error("bad icon accepted")
		}
	}
}

func TestPack(t *testing.T) {
	data, err := Pack(filepath.Join("testdata", "example"), "")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ReadZip(data, "")
	if err != nil {
		t.Fatal(err)
	}
	dir, _ := ReadDir(filepath.Join("testdata", "example"), "")
	if len(pkg.Files) != len(dir.Files) || pkg.Manifest.ID != "example" {
		t.Fatalf("packed %v", keys(pkg.Files))
	}
}
