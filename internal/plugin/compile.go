package plugin

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/MoeForge/moejs"
)

// compileTimeout bounds compiling one package, whose source the host does
// not trust.
const compileTimeout = 20 * time.Second

// compile compiles the entry module of pkg and the modules it imports,
// links them, and checks the exports the manifest needs (§5), without
// running any of it. Imports must be relative ("./" or "../") and stay
// inside the package.
func compile(pkg *Package) (*moejs.Module, error) {
	ctx, cancel := context.WithTimeout(context.Background(), compileTimeout)
	defer cancel()
	mods := map[string]*moejs.Module{}
	load := func(name string) (*moejs.Module, error) {
		if m, ok := mods[name]; ok {
			return m, nil
		}
		src, ok := pkg.Files[name]
		if !ok {
			return nil, fmt.Errorf("找不到模块 %s", name)
		}
		m, err := moejs.CompileWith(name, string(src), moejs.CompileOptions{Context: ctx})
		if err != nil {
			return nil, err
		}
		mods[name] = m
		return m, nil
	}
	entry, err := load(pkg.Manifest.Main)
	if err != nil {
		return nil, err
	}
	linked, err := moejs.Link(entry, func(ref moejs.Referrer, spec string) (*moejs.Module, error) {
		name, err := resolveImport(ref.Name(), spec)
		if err != nil {
			return nil, err
		}
		return load(name)
	})
	if err != nil {
		return nil, err
	}
	exports := linked.Exports()
	var missing []string
	for _, fn := range pkg.Manifest.Functions() {
		if !slices.Contains(exports, fn) {
			missing = append(missing, fn)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("没有导出 %s", strings.Join(missing, "、"))
	}
	return linked, nil
}

// resolveImport resolves spec imported from the module named from.
func resolveImport(from, spec string) (string, error) {
	if !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") {
		return "", fmt.Errorf("只能用 ./ 或 ../ 开头的相对路径导入，不能导入 %q", spec)
	}
	if strings.ContainsAny(spec, `\?#`) {
		return "", fmt.Errorf("导入路径 %q 不合格", spec)
	}
	joined := path.Join(path.Dir(from), spec)
	if joined == ".." || strings.HasPrefix(joined, "../") || strings.HasPrefix(joined, "/") {
		return "", errors.New("不能导入插件包以外的模块")
	}
	if err := checkPath(joined); err != nil {
		return "", err
	}
	return joined, nil
}
