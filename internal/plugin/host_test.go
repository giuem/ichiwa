package plugin

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/giuem/ichiwa/internal/source"
)

func hostCall(t *testing.T, p *Plugin, id string, args map[string]string) map[string]any {
	t.Helper()
	raw, err := p.Call(context.Background(), "collection", map[string]any{"id": id, "filters": args})
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	var v map[string]any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func TestHostDigest(t *testing.T) {
	v := hostCall(t, open(t, "hostapi", Options{}), "digest", nil)
	want := map[string]string{
		"md5":    "900150983cd24fb0d6963f7d28e17f72",
		"sha1":   "a9993e364706816aba3e25717850c26c9cd0d89d",
		"sha256": "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad",
		"hmac":   "f7bc83f430538424b13298e6aa6fb143ef4d59a14946175997479dbc2d1a3cd8",
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %v, want %s", k, v[k], w)
		}
	}
}

func TestHostAES(t *testing.T) {
	v := hostCall(t, open(t, "hostapi", Options{}), "aes", nil)
	for _, mode := range []string{"ecb", "cbc", "ctr", "gcm"} {
		m := v[mode].(map[string]any)
		if m["dec"] != "你好，一集" || m["isU8"] != true || m["enc"] == "" {
			t.Errorf("%s = %v", mode, m)
		}
	}
	// AES-128 of a zero block with key 00..0f (FIPS-197 style check).
	if v["nopad"] != "c6a13b37878f5b826f4f8162a1c8d879" {
		t.Errorf("ecb no padding = %v", v["nopad"])
	}
	if !strings.Contains(v["badKey"].(string), "16、24 或 32") {
		t.Errorf("bad key = %v", v["badKey"])
	}
}

func TestHostRSA(t *testing.T) {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	der, _ := x509.MarshalPKIXPublicKey(&key.PublicKey)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
	p := open(t, "hostapi", Options{})
	for _, c := range []struct{ key, padding string }{
		{pemKey, "pkcs1"},
		{pemKey, "oaep-sha1"},
		{base64.StdEncoding.EncodeToString(x509.MarshalPKCS1PublicKey(&key.PublicKey)), "oaep-sha256"},
	} {
		v := hostCall(t, p, "rsa", map[string]string{"key": c.key, "padding": c.padding})
		enc, _ := base64.StdEncoding.DecodeString(v["enc"].(string))
		var out []byte
		var err error
		switch c.padding {
		case "pkcs1":
			out, err = rsa.DecryptPKCS1v15(nil, key, enc)
		case "oaep-sha1":
			out, err = rsa.DecryptOAEP(sha1.New(), nil, key, enc, nil)
		default:
			out, err = rsa.DecryptOAEP(sha256.New(), nil, key, enc, nil)
		}
		if err != nil || string(out) != "secret" {
			t.Errorf("%s: %q, %v", c.padding, out, err)
		}
	}
}

func TestHostRandomAndUUID(t *testing.T) {
	v := hostCall(t, open(t, "hostapi", Options{}), "random", nil)
	if v["len"] != float64(32) || !strings.Contains(v["tooMany"].(string), "1024") {
		t.Errorf("random = %v", v)
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(v["uuid"].(string)) {
		t.Errorf("uuid = %v", v["uuid"])
	}
}

func TestHostEncoding(t *testing.T) {
	var gz, zl, raw bytes.Buffer
	w := gzip.NewWriter(&gz)
	w.Write([]byte("gzip 内容"))
	w.Close()
	z := zlib.NewWriter(&zl)
	z.Write([]byte("zlib 内容"))
	z.Close()
	f, _ := flate.NewWriter(&raw, flate.BestSpeed)
	f.Write([]byte("raw 内容"))
	f.Close()
	b64 := base64.StdEncoding.EncodeToString
	v := hostCall(t, open(t, "hostapi", Options{}), "encoding", map[string]string{"gz": b64(gz.Bytes()), "zlib": b64(zl.Bytes()), "raw": b64(raw.Bytes())})
	want := map[string]string{
		"hex": "6869", "fromHex": "hi", "b64": "+/+/", "fromB64": "fbffbf", "fromB64Std": "fbffbf",
		"gunzip": "gzip 内容", "inflate": "zlib 内容", "inflateRaw": "raw 内容",
	}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %v, want %s", k, v[k], w)
		}
	}
}

func TestHostDecompressionBomb(t *testing.T) {
	var gz bytes.Buffer
	w, _ := gzip.NewWriterLevel(&gz, gzip.BestCompression)
	w.Write(make([]byte, MaxInflateBytes+1))
	w.Close()
	v := hostCall(t, open(t, "hostapi", Options{}), "bomb", map[string]string{"gz": base64.StdEncoding.EncodeToString(gz.Bytes())})
	if !strings.Contains(v["error"].(string), "超过") {
		t.Fatalf("bomb = %v", v)
	}
}

func TestHostDevice(t *testing.T) {
	v := hostCall(t, open(t, "hostapi", Options{AppVersion: "2026.1127.1"}), "device", nil)
	if v["appVersion"] != "2026.1127.1" || v["locale"] != "zh-CN" || v["platform"] != platformName() {
		t.Fatalf("device = %v", v)
	}
}

func TestHostCache(t *testing.T) {
	p := open(t, "hostapi", Options{})
	if v := hostCall(t, p, "cache", nil); v["before"] != nil || v["after"] != "cached" || !strings.Contains(v["big"].(string), "256") {
		t.Fatalf("cache = %v", v)
	}
	// The cache outlives the call, not the plugin.
	if v := hostCall(t, p, "cache", nil); v["before"] != "cached" {
		t.Fatalf("cache across calls = %v", v)
	}
}

func TestHostWebAPIs(t *testing.T) {
	v := hostCall(t, open(t, "hostapi", Options{}), "web", nil)
	want := map[string]any{"url": "https://example.com/a?b=1", "params": "q=%E4%B8%80%E9%9B%86", "text": "编码", "b64": "aGk=", "abort": "function", "headers": "1"}
	for k, w := range want {
		if v[k] != w {
			t.Errorf("%s = %v, want %v", k, v[k], w)
		}
	}
}

func TestConsoleLogs(t *testing.T) {
	p := open(t, "hostapi", Options{})
	hostCall(t, p, "console", nil)
	text := p.Logs().Text()
	for _, s := range []string{`[log] 一行 1 {"a":2} [3]`, "[warn] 警告", "[error] Error: 错误"} {
		if !strings.Contains(text, s) {
			t.Errorf("log lacks %q:\n%s", s, text)
		}
	}
}

func TestLogRing(t *testing.T) {
	var l Logs
	line := strings.Repeat("x", 1000)
	for range 3000 {
		l.add("log", line)
	}
	es := l.Entries()
	total := 0
	for _, e := range es {
		total += len(e.Text) + logEntryCost
	}
	if total > MaxLogBytes || total < MaxLogBytes-1100 {
		t.Fatalf("log holds %d bytes", total)
	}
	if tail := l.Tail(5); len(tail) != 5 || tail[4] != es[len(es)-1] {
		t.Fatalf("tail = %d entries", len(tail))
	}
	l.add("log", strings.Repeat("y", 3*maxLogLine))
	last := l.Entries()[len(l.Entries())-1]
	if len(last.Text) > maxLogLine+64 {
		t.Fatalf("a long line kept %d bytes", len(last.Text))
	}
	got := 0
	stop := l.Watch(func(LogEntry) { got++ })
	l.add("log", "a")
	stop()
	l.add("log", "b")
	if got != 1 {
		t.Fatalf("watch saw %d entries", got)
	}
}

// Empty lines take room too: a flood of them is dropped like any other.
func TestLogFlood(t *testing.T) {
	p := open(t, "hostile/loop", Options{Timeout: 300 * time.Millisecond})
	for range 3 {
		if _, err := p.Collection(context.Background(), source.CollectionRequest{ID: "spam"}); code(err) != source.ErrPlugin {
			t.Fatalf("spam: %v", err)
		}
	}
	if n := len(p.Logs().Entries()); n > MaxLogBytes/logEntryCost {
		t.Fatalf("log holds %d empty lines", n)
	}
}

func TestCacheLimits(t *testing.T) {
	c := NewCache()
	now := time.Unix(0, 0)
	c.now = func() time.Time { return now }
	c.Set("t", "v", time.Second)
	now = now.Add(2 * time.Second)
	if _, ok := c.Get("t"); ok {
		t.Fatal("expired item returned")
	}
	item := strings.Repeat("x", MaxCacheItemBytes-10)
	for i := range 20 {
		if err := c.Set(string(rune('a'+i)), item, 0); err != nil {
			t.Fatal(err)
		}
	}
	if c.size > MaxCacheBytes {
		t.Fatalf("cache holds %d bytes", c.size)
	}
	if _, ok := c.Get("a"); ok {
		t.Fatal("the oldest item was kept past the limit")
	}
	if _, ok := c.Get("t" + "x"); ok {
		t.Fatal("never set")
	}
	if v, ok := c.Get(string(rune('a' + 19))); !ok || v != item {
		t.Fatal("the newest item is gone")
	}
}

// A key whose modulus is huge is refused at once: encrypting with it
// could not be interrupted.
func TestRSARefusesHugeKeys(t *testing.T) {
	for _, bits := range []uint{maxRSABits + 8, 512 << 13} {
		n := new(big.Int).Lsh(big.NewInt(1), bits-1)
		n.Add(n, big.NewInt(1))
		der := x509.MarshalPKCS1PublicKey(&rsa.PublicKey{N: n, E: 0x7fffffff})
		start := time.Now()
		if _, err := rsaEncrypt(base64.StdEncoding.EncodeToString(der), []byte("x"), "pkcs1"); err == nil {
			t.Errorf("%d-bit key used", bits)
		}
		if d := time.Since(start); d > time.Second {
			t.Errorf("%d-bit key took %v", bits, d)
		}
	}
}
