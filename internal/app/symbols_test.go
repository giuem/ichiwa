package app

import (
	"os"
	"regexp"
	"testing"
)

// Without a fallback a symbol is blank outside macOS.
func TestSymbolsHaveFallbacks(t *testing.T) {
	names := regexp.MustCompile(`"[a-z]+(\.[a-z0-9]+)+"`)
	for _, file := range []string{"kit.go", "watch.go"} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range names.FindAll(src, -1) {
			if name := string(q[1 : len(q)-1]); symbolFallbacks[name] == nil {
				t.Errorf("%s: no fallback for %q", file, name)
			}
		}
	}
}
