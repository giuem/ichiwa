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

// -cache keeps host.cache, with its expiry, between runs: a lock taken
// in one run is still held in the next.
func TestCallKeepsCache(t *testing.T) {
	file := filepath.Join(t.TempDir(), "cache.json")
	hostapi := filepath.Join("..", "..", "internal", "plugin", "testdata", "hostapi")
	run := func() map[string]any {
		t.Helper()
		var out, logs bytes.Buffer
		if err := call([]string{"-raw", "-cache", file, hostapi, "collection", `{"id":"cache"}`}, &out, &logs); err != nil {
			t.Fatalf("call: %v\n%s", err, logs.String())
		}
		var v map[string]any
		if err := json.Unmarshal(out.Bytes(), &v); err != nil {
			t.Fatalf("%v: %s", err, out.String())
		}
		return v
	}
	if v := run(); v["before"] != nil || v["first"] != true {
		t.Fatalf("first run = %v", v)
	}
	if v := run(); v["before"] != "cached" || v["first"] != false || v["lock"] != "a" {
		t.Fatalf("second run = %v", v)
	}
}

// A cache that can't be saved fails the run, after printing its result.
func TestCallFailsUnsavedCache(t *testing.T) {
	file := filepath.Join(t.TempDir(), "missing", "cache.json")
	hostapi := filepath.Join("..", "..", "internal", "plugin", "testdata", "hostapi")
	var out, logs bytes.Buffer
	err := call([]string{"-raw", "-cache", file, hostapi, "collection", `{"id":"cache"}`}, &out, &logs)
	if err == nil || !strings.Contains(err.Error(), "保存 host.cache 失败") {
		t.Fatalf("call = %v", err)
	}
	if !strings.Contains(out.String(), `"first": true`) {
		t.Fatalf("output = %s", out.String())
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
