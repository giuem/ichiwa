package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const example = "../../internal/plugin/testdata/example"

func TestCheck(t *testing.T) {
	var out bytes.Buffer
	if err := check([]string{example}, &out); err != nil || !strings.Contains(out.String(), "检查通过") {
		t.Fatalf("check: %v\n%s", err, out.String())
	}
	if err := check([]string{"../../internal/plugin/testdata/hostile/missing"}, &out); err == nil {
		t.Fatal("a plugin missing an export passed")
	}
}

// Every function of the example plugin can be called.
func TestCallEveryFunction(t *testing.T) {
	calls := [][]string{
		{"navigation"},
		{"collection", `{"id":"home"}`},
		{"collection", `{"id":"filter","filters":{"genre":"奇幻"}}`},
		{"media", `{"id":"bunny"}`},
		{"play", `{"mediaId":"bunny","episodeId":"bunny-1","prefer":{"maxHeight":1080,"hevc":true}}`},
		{"search", `{"query":"兔子"}`},
		{"searchHome"},
		{"suggest", `{"query":"兔"}`},
		{"danmaku", `{"mediaId":"bunny","episodeId":"bunny-1"}`},
		{"refresh", `{"ids":["bunny","sintel"]}`},
	}
	for _, c := range calls {
		var out, logs bytes.Buffer
		if err := call(append([]string{example}, c...), &out, &logs); err != nil {
			t.Errorf("%v: %v\n%s", c, err, logs.String())
			continue
		}
		var v any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil || v == nil {
			t.Errorf("%v printed %q", c, out.String())
		}
		if !strings.Contains(logs.String(), "[host] "+c[0]+" 用时") {
			t.Errorf("%v: the log is not printed:\n%s", c, logs.String())
		}
	}
	// The raw JSON, as the plugin returned it.
	var out, logs bytes.Buffer
	if err := call([]string{"-raw", example, "suggest", `{"query":"辛"}`}, &out, &logs); err != nil || !strings.Contains(out.String(), `"辛特尔"`) {
		t.Fatalf("raw: %v %s", err, out.String())
	}
	// An error code reaches the developer.
	if err := call([]string{example, "media", `{"id":"nope"}`}, &out, &logs); err == nil || !strings.Contains(err.Error(), "notFound") {
		t.Fatalf("error: %v", err)
	}
}

func TestCallKeepsStorage(t *testing.T) {
	file := filepath.Join(t.TempDir(), "storage.json")
	for range 2 {
		var out, logs bytes.Buffer
		if err := call([]string{"-storage", file, example, "navigation"}, &out, &logs); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(file)
	if !strings.Contains(string(data), `"launches":"2"`) {
		t.Fatalf("storage = %s", data)
	}
}

func TestPack(t *testing.T) {
	zip := filepath.Join(t.TempDir(), "plugin.zip")
	var out bytes.Buffer
	if err := pack([]string{"-o", zip, example}, &out); err != nil {
		t.Fatal(err)
	}
	if err := check([]string{zip}, &out); err != nil {
		t.Fatalf("the packed zip: %v", err)
	}
}
