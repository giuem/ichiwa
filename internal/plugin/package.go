package plugin

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // icon formats (§4)
	_ "image/png"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	_ "golang.org/x/image/webp"
)

// Package limits (§4).
const (
	MaxZipBytes      = 8 << 20
	MaxUnpackedBytes = 32 << 20
	maxFiles         = 2000
	minIconSize      = 128
)

// Package is a plugin's files, read and checked but not run.
type Package struct {
	Manifest *Manifest
	Files    map[string][]byte // by slash path, relative to the root
	Icon     []byte            // nil when the plugin has none
	// Digest is the SHA-256 of the zip, or of the files of a folder.
	Digest [sha256.Size]byte
}

// checkPath refuses a path inside a package that could leave it or
// confuse a file system (§4): absolute paths, "..", backslashes,
// control characters and empty segments.
func checkPath(p string) error {
	switch {
	case p == "":
		return errors.New("空路径")
	case !utf8.ValidString(p):
		return fmt.Errorf("路径 %q 不是有效的 UTF-8", p)
	case strings.Contains(p, `\`):
		return fmt.Errorf("路径 %q 含有反斜杠", p)
	case strings.HasPrefix(p, "/"):
		return fmt.Errorf("路径 %q 不能是绝对路径", p)
	}
	for _, r := range p {
		if unicode.IsControl(r) {
			return fmt.Errorf("路径 %q 含有控制字符", p)
		}
	}
	for _, seg := range strings.Split(p, "/") {
		switch seg {
		case "", ".", "..":
			return fmt.Errorf("路径 %q 含有 %q", p, seg)
		}
		if strings.Contains(seg, ":") {
			return fmt.Errorf("路径 %q 含有冒号", p)
		}
	}
	return nil
}

// files collects a package's files, refusing case-insensitive duplicates
// and enforcing the unpacked size limit.
type files struct {
	m     map[string][]byte
	lower map[string]string
	total int64
}

func newFiles() *files { return &files{m: map[string][]byte{}, lower: map[string]string{}} }

func (f *files) add(name string, r io.Reader) error {
	if err := checkPath(name); err != nil {
		return err
	}
	low := strings.ToLower(name)
	if prev, ok := f.lower[low]; ok {
		return fmt.Errorf("%q 和 %q 只差大小写", prev, name)
	}
	// Every directory of the path must not clash with a file either.
	for dir := path.Dir(name); dir != "."; dir = path.Dir(dir) {
		if prev, ok := f.lower[strings.ToLower(dir)]; ok {
			return fmt.Errorf("%q 和目录 %q 冲突", prev, dir)
		}
	}
	if len(f.m) >= maxFiles {
		return fmt.Errorf("文件太多，最多 %d 个", maxFiles)
	}
	left := MaxUnpackedBytes - f.total
	data, err := io.ReadAll(io.LimitReader(r, left+1))
	if err != nil {
		return fmt.Errorf("读取 %s：%w", name, err)
	}
	if int64(len(data)) > left {
		return fmt.Errorf("解压后超过 %d MiB", MaxUnpackedBytes>>20)
	}
	f.total += int64(len(data))
	f.lower[low] = name
	f.m[name] = data
	return nil
}

// ReadZip reads a plugin.zip (§4). appVersion is passed to ParseManifest.
func ReadZip(data []byte, appVersion string) (*Package, error) {
	if len(data) > MaxZipBytes {
		return nil, fmt.Errorf("插件包超过 %d MiB", MaxZipBytes>>20)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("插件包不是有效的 zip：%w", err)
	}
	fs := newFiles()
	for _, zf := range zr.File {
		name := zf.Name
		mode := zf.Mode()
		if strings.HasSuffix(name, "/") && mode.IsDir() {
			if err := checkPath(strings.TrimSuffix(name, "/")); err != nil {
				return nil, err
			}
			continue
		}
		if mode&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("插件包不能含符号链接：%s", name)
		}
		if !mode.IsRegular() {
			return nil, fmt.Errorf("插件包含有不是普通文件的 %s", name)
		}
		rc, err := zf.Open()
		if err != nil {
			return nil, fmt.Errorf("读取 %s：%w", name, err)
		}
		err = fs.add(name, rc)
		rc.Close()
		if err != nil {
			return nil, err
		}
	}
	return finish(fs.m, sha256.Sum256(data), appVersion)
}

// skipDir tells the folders of a plugin folder that are not part of it:
// hidden ones (.git) and node_modules, which a bundler reads, not the
// plugin. pack leaves them out the same way.
func skipEntry(name string) bool {
	return strings.HasPrefix(name, ".") || name == "node_modules"
}

// ReadDir reads a plugin from a folder, as the developer mode and the
// command line tool do. Symbolic links are refused, as in a zip.
func ReadDir(dir string, appVersion string) (*Package, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	fs := newFiles()
	err = walk(root, ".", fs)
	if err != nil {
		return nil, err
	}
	return finish(fs.m, digestFiles(fs.m), appVersion)
}

func walk(root *os.Root, dir string, out *files) error {
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	entries, err := f.ReadDir(-1)
	f.Close()
	if err != nil {
		return err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, e := range entries {
		name := e.Name()
		if skipEntry(name) {
			continue
		}
		rel := path.Join(dir, name)
		info, err := root.Lstat(rel)
		if err != nil {
			return err
		}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("插件不能含符号链接：%s", rel)
		case info.IsDir():
			if err := checkPath(rel); err != nil {
				return err
			}
			if err := walk(root, rel, out); err != nil {
				return err
			}
		case info.Mode().IsRegular():
			fh, err := root.Open(rel)
			if err != nil {
				return err
			}
			err = out.add(rel, fh)
			fh.Close()
			if err != nil {
				return err
			}
		default:
			return fmt.Errorf("插件含有不是普通文件的 %s", rel)
		}
	}
	return nil
}

func digestFiles(m map[string][]byte) [sha256.Size]byte {
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		fmt.Fprintf(h, "%d:%s:%d:", len(n), n, len(m[n]))
		h.Write(m[n])
	}
	var out [sha256.Size]byte
	copy(out[:], h.Sum(nil))
	return out
}

func finish(m map[string][]byte, digest [sha256.Size]byte, appVersion string) (*Package, error) {
	mf, ok := m["plugin.json"]
	if !ok {
		return nil, errors.New("根目录没有 plugin.json")
	}
	man, err := ParseManifest(mf, appVersion)
	if err != nil {
		return nil, err
	}
	if _, ok := m[man.Main]; !ok {
		return nil, fmt.Errorf("main 指向的 %s 不存在", man.Main)
	}
	pkg := &Package{Manifest: man, Files: m, Digest: digest}
	if man.Icon != "" {
		data, ok := m[man.Icon]
		if !ok {
			return nil, fmt.Errorf("icon 指向的 %s 不存在", man.Icon)
		}
		if err := checkIcon(data); err != nil {
			return nil, err
		}
		pkg.Icon = data
	}
	return pkg, nil
}

func checkIcon(data []byte) error {
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("图标不是 PNG、JPEG 或 WebP：%w", err)
	}
	switch format {
	case "png", "jpeg", "webp":
	default:
		return fmt.Errorf("图标格式 %s 不支持", format)
	}
	if cfg.Width != cfg.Height {
		return fmt.Errorf("图标必须是正方形，现在是 %d×%d", cfg.Width, cfg.Height)
	}
	if cfg.Width < minIconSize {
		return fmt.Errorf("图标至少 %d×%d", minIconSize, minIconSize)
	}
	return nil
}

// Pack writes the files of the plugin folder dir as a zip, after the
// same checks ReadDir makes, and returns its bytes.
func Pack(dir string, appVersion string) ([]byte, error) {
	pkg, err := ReadDir(dir, appVersion)
	if err != nil {
		return nil, err
	}
	if _, err := compile(pkg); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(pkg.Files))
	for n := range pkg.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, n := range names {
		h := &zip.FileHeader{Name: n, Method: zip.Deflate}
		h.SetMode(0o644)
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(pkg.Files[n]); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	if buf.Len() > MaxZipBytes {
		return nil, fmt.Errorf("打出的包超过 %d MiB", MaxZipBytes>>20)
	}
	return buf.Bytes(), nil
}

// ReadPath reads a plugin from a folder or a zip file.
func ReadPath(p string, appVersion string) (*Package, error) {
	info, err := os.Stat(p)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return ReadDir(p, appVersion)
	}
	if info.Size() > MaxZipBytes {
		return nil, fmt.Errorf("插件包超过 %d MiB", MaxZipBytes>>20)
	}
	data, err := os.ReadFile(filepath.Clean(p))
	if err != nil {
		return nil, err
	}
	return ReadZip(data, appVersion)
}
