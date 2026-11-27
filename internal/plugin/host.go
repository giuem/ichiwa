package plugin

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/MoeForge/moejs"
	"github.com/MoeForge/moejs/engine"
)

// helperSource builds, in each runtime and before the plugin's code runs,
// the globals host and console (§8) over the natives Go gives it, and the
// function that calls an export. Errors made by host.error are marked in
// a WeakSet the plugin cannot reach, so that only they carry an error
// code (§10). So are the errors fetch rejects with, so that a call that
// fails with one, uncaught, is a network error rather than the plugin's.
const helperSource = `"use strict";
const marks = new WeakSet();
const fetchErrors = new WeakSet();
const realFetch = globalThis.fetch;
if (typeof realFetch === "function") {
  const fetch = function fetch(input, init) {
    return realFetch(input, init).then(undefined, (e) => {
      if (e !== null && (typeof e === "object" || typeof e === "function")) fetchErrors.add(e);
      throw e;
    });
  };
  Object.defineProperty(globalThis, "fetch", { value: fetch, writable: true, configurable: true, enumerable: false });
}
const str = (v) => String(v);
const freeze = Object.freeze;
const host = freeze({
  storage: freeze({
    get: (k) => native.storageGet(str(k)),
    set: (k, v) => { native.storageSet(str(k), str(v)); },
    delete: (k) => { native.storageDelete(str(k)); },
  }),
  cache: freeze({
    get: (k) => native.cacheGet(str(k)),
    set: (k, v, ttl) => { native.cacheSet(str(k), str(v), ttl === undefined || ttl === null ? 0 : Number(ttl)); },
    add: (k, v, ttl) => native.cacheAdd(str(k), str(v), ttl === undefined || ttl === null ? 0 : Number(ttl)),
    delete: (k) => { native.cacheDelete(str(k)); },
  }),
  crypto: freeze({
    md5: (d) => native.digest("md5", d),
    sha1: (d) => native.digest("sha1", d),
    sha256: (d) => native.digest("sha256", d),
    hmac: (alg, key, data) => native.hmac(str(alg), key, data),
    aes: (o) => native.aes(o),
    rsa: (o) => native.rsa(o),
    randomBytes: (n) => native.randomBytes(Number(n)),
    uuid: () => native.uuid(),
  }),
  encoding: freeze({
    hex: (b) => native.hex(b),
    fromHex: (s) => native.fromHex(str(s)),
    base64: (b) => native.base64(b),
    fromBase64: (s) => native.fromBase64(str(s)),
    utf8: (s) => new TextEncoder().encode(str(s)),
    fromUtf8: (b) => new TextDecoder().decode(b),
    gunzip: (b) => native.gunzip(b),
    inflate: (b, raw) => native.inflate(b, !!raw),
  }),
  device: () => ({ platform: native.platform, appVersion: native.appVersion, locale: native.locale }),
  error: (code, message, extra) => {
    const e = new Error(message === undefined || message === null ? "" : str(message));
    e.name = "HostError";
    e.code = str(code);
    if (extra !== null && typeof extra === "object" && typeof extra.retryAfter === "number") e.retryAfter = extra.retryAfter;
    marks.add(e);
    return e;
  },
});
const show = (a) => {
  try {
    if (typeof a === "string") return a;
    if (a instanceof Error) return a.stack || str(a);
    const s = JSON.stringify(a);
    return s === undefined ? str(a) : s;
  } catch { try { return str(a); } catch { return "[无法显示]"; } }
};
const say = (level) => (...args) => { native.log(level, args.map(show).join(" ")); };
const console = freeze({ log: say("log"), info: say("info"), warn: say("warn"), error: say("error"), debug: say("debug"), trace: say("debug") });
const describe = (e) => {
  try {
    if (marks.has(e)) {
      return { host: true, code: str(e.code), message: str(e.message), retryAfter: typeof e.retryAfter === "number" ? e.retryAfter : 0 };
    }
    return { host: false, fetch: fetchErrors.has(e), text: e instanceof Error ? (e.stack || str(e)) : show(e) };
  } catch { return { host: false, text: "无法读取的异常" }; }
};
const invoke = (fn, arg, resolve, reject) => {
  try {
    Promise.resolve(fn(arg)).then(resolve, (e) => reject(describe(e)));
  } catch (e) {
    reject(describe(e));
  }
};
return { host, console, invoke };
`

var helperScript = func() *moejs.Script {
	s, err := moejs.CompileFunction("ichiwa:host", "native", helperSource, moejs.FunctionOptions{})
	if err != nil {
		panic(err)
	}
	return s
}()

func platformName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "ios":
		return "tvos"
	default:
		return runtime.GOOS
	}
}

// callState is what the natives of one call share.
type callState struct {
	p  *Plugin
	rt *moejs.Runtime
}

func (c *callState) str(v moejs.Value) string {
	if v.IsUndefined() || v.IsNull() {
		return ""
	}
	return v.String()
}

// bytesArg reads a Bytes argument: a string as UTF-8, or the bytes of a
// Uint8Array, ArrayBuffer or other view.
func (c *callState) bytesArg(v moejs.Value, name string) ([]byte, error) {
	if v.IsString() {
		return []byte(v.String()), nil
	}
	if v.IsObject() {
		g, err := c.rt.ToGo(v)
		if err == nil {
			if b, ok := g.([]byte); ok {
				return b, nil
			}
		}
	}
	return nil, fmt.Errorf("%s 必须是字符串或 Uint8Array", name)
}

// u8 hands b to JavaScript as a Uint8Array, charged to the memory limit.
func (c *callState) u8(r *moejs.Realm, b []byte) (moejs.Value, error) {
	if err := r.ChargeMemory(len(b)); err != nil {
		return moejs.Undefined(), err
	}
	obj, err := r.NewUint8Array(b, nil)
	if err != nil {
		return moejs.Undefined(), err
	}
	return engine.ObjectValue(obj), nil
}

// strValue hands s to JavaScript, charged to the memory limit: a string
// a native makes is memory the plugin did not allocate itself.
func (c *callState) strValue(r *moejs.Realm, s string) (moejs.Value, error) {
	if err := r.ChargeMemory(len(s)); err != nil {
		return moejs.Undefined(), err
	}
	return moejs.String(s), nil
}

type native = func(r *moejs.Realm, args []moejs.Value) (moejs.Value, error)

func (c *callState) natives() map[string]any {
	p := c.p
	fn := func(name string, n int, f native) moejs.Value {
		return c.rt.Function(name, n, func(r *moejs.Realm, _ moejs.Value, args []moejs.Value) (moejs.Value, error) {
			return f(r, args)
		})
	}
	arg := moejs.Arg
	return map[string]any{
		"platform":   platformName(),
		"appVersion": p.opts.AppVersion,
		"locale":     p.locale(),
		"log": fn("log", 2, func(_ *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			p.logs.add(c.str(arg(a, 0)), c.str(arg(a, 1)))
			return moejs.Undefined(), nil
		}),
		"storageGet": fn("get", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			if v, ok := p.storage.Get(c.str(arg(a, 0))); ok {
				return c.strValue(r, v)
			}
			return moejs.Undefined(), nil
		}),
		"storageSet": fn("set", 2, func(_ *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			return moejs.Undefined(), p.storage.Set(c.str(arg(a, 0)), c.str(arg(a, 1)))
		}),
		"storageDelete": fn("delete", 1, func(_ *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			return moejs.Undefined(), p.storage.Delete(c.str(arg(a, 0)))
		}),
		"cacheGet": fn("get", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			if v, ok := p.cache.Get(c.str(arg(a, 0))); ok {
				return c.strValue(r, v)
			}
			return moejs.Undefined(), nil
		}),
		"cacheSet": fn("set", 3, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			d, err := cacheTTL(r, arg(a, 2))
			if err != nil {
				return moejs.Undefined(), err
			}
			return moejs.Undefined(), p.cache.Set(c.str(arg(a, 0)), c.str(arg(a, 1)), d)
		}),
		"cacheAdd": fn("add", 3, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			d, err := cacheTTL(r, arg(a, 2))
			if err != nil {
				return moejs.Undefined(), err
			}
			added, err := p.cache.Add(c.str(arg(a, 0)), c.str(arg(a, 1)), d)
			if err != nil {
				return moejs.Undefined(), err
			}
			return moejs.Bool(added), nil
		}),
		"cacheDelete": fn("delete", 1, func(_ *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			p.cache.Delete(c.str(arg(a, 0)))
			return moejs.Undefined(), nil
		}),
		"digest": fn("digest", 2, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			data, err := c.bytesArg(arg(a, 1), "data")
			if err != nil {
				return moejs.Undefined(), err
			}
			s, err := digestHex(c.str(arg(a, 0)), data)
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.strValue(r, s)
		}),
		"hmac": fn("hmac", 3, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			key, err := c.bytesArg(arg(a, 1), "key")
			if err != nil {
				return moejs.Undefined(), err
			}
			data, err := c.bytesArg(arg(a, 2), "data")
			if err != nil {
				return moejs.Undefined(), err
			}
			s, err := hmacHex(c.str(arg(a, 0)), key, data)
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.strValue(r, s)
		}),
		"aes": fn("aes", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			o := arg(a, 0)
			if !o.IsObject() {
				return moejs.Undefined(), errors.New("aes 需要一个选项对象")
			}
			get := func(k string) moejs.Value { v, _ := c.rt.Get(o, k); return v }
			var opts aesOptions
			var err error
			opts.Mode = c.str(get("mode"))
			if opts.Key, err = c.bytesArg(get("key"), "key"); err != nil {
				return moejs.Undefined(), err
			}
			if iv := get("iv"); !iv.IsUndefined() && !iv.IsNull() {
				if opts.IV, err = c.bytesArg(iv, "iv"); err != nil {
					return moejs.Undefined(), err
				}
			}
			if opts.Data, err = c.bytesArg(get("data"), "data"); err != nil {
				return moejs.Undefined(), err
			}
			if aad := get("aad"); !aad.IsUndefined() && !aad.IsNull() {
				if opts.AAD, err = c.bytesArg(aad, "aad"); err != nil {
					return moejs.Undefined(), err
				}
			}
			opts.Decrypt = engine.ToBoolean(get("decrypt"))
			opts.Padding = c.str(get("padding"))
			out, err := aesCrypt(opts)
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.u8(r, out)
		}),
		"rsa": fn("rsa", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			o := arg(a, 0)
			if !o.IsObject() {
				return moejs.Undefined(), errors.New("rsa 需要一个选项对象")
			}
			get := func(k string) moejs.Value { v, _ := c.rt.Get(o, k); return v }
			data, err := c.bytesArg(get("data"), "data")
			if err != nil {
				return moejs.Undefined(), err
			}
			out, err := rsaEncrypt(c.str(get("publicKey")), data, c.str(get("padding")))
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.u8(r, out)
		}),
		"randomBytes": fn("randomBytes", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			n, _ := r.ToNumber(arg(a, 0))
			if math.IsNaN(n) || n != math.Trunc(n) {
				return moejs.Undefined(), errors.New("randomBytes 的 n 必须是整数")
			}
			b, err := randomBytes(int(n))
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.u8(r, b)
		}),
		"uuid": fn("uuid", 0, func(_ *moejs.Realm, _ []moejs.Value) (moejs.Value, error) {
			return moejs.String(uuid4()), nil
		}),
		"hex": fn("hex", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := c.bytesArg(arg(a, 0), "b")
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.strValue(r, hex.EncodeToString(b))
		}),
		"fromHex": fn("fromHex", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := hex.DecodeString(c.str(arg(a, 0)))
			if err != nil {
				return moejs.Undefined(), errors.New("不是有效的十六进制")
			}
			return c.u8(r, b)
		}),
		"base64": fn("base64", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := c.bytesArg(arg(a, 0), "b")
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.strValue(r, base64.StdEncoding.EncodeToString(b))
		}),
		"fromBase64": fn("fromBase64", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := decodeBase64(c.str(arg(a, 0)))
			if err != nil {
				return moejs.Undefined(), errors.New("不是有效的 base64")
			}
			return c.u8(r, b)
		}),
		"gunzip": fn("gunzip", 1, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := c.bytesArg(arg(a, 0), "b")
			if err != nil {
				return moejs.Undefined(), err
			}
			out, err := gunzip(b)
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.u8(r, out)
		}),
		"inflate": fn("inflate", 2, func(r *moejs.Realm, a []moejs.Value) (moejs.Value, error) {
			b, err := c.bytesArg(arg(a, 0), "b")
			if err != nil {
				return moejs.Undefined(), err
			}
			out, err := inflate(b, engine.ToBoolean(arg(a, 1)))
			if err != nil {
				return moejs.Undefined(), err
			}
			return c.u8(r, out)
		}),
	}
}

// cacheTTL reads host.cache's ttlSeconds: 0 for none, at most a year.
func cacheTTL(r *moejs.Realm, v moejs.Value) (time.Duration, error) {
	ttl, _ := r.ToNumber(v)
	if math.IsNaN(ttl) || ttl < 0 {
		return 0, errors.New("ttlSeconds 必须是非负数")
	}
	return time.Duration(math.Min(ttl, 365*24*3600) * float64(time.Second)), nil
}
