// 宿主 API：每个 id 跑一项，把结果原样返回给测试比对。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }

const hex = (b) => host.encoding.hex(b);

export function collection(req) {
  const a = req.filters ?? {};
  switch (req.id) {
    case "digest":
      return {
        md5: host.crypto.md5("abc"),
        sha1: host.crypto.sha1(host.encoding.utf8("abc")),
        sha256: host.crypto.sha256("abc"),
        hmac: host.crypto.hmac("sha256", "key", "The quick brown fox jumps over the lazy dog"),
      };
    case "aes": {
      const key = host.encoding.fromHex("000102030405060708090a0b0c0d0e0f");
      const iv = host.encoding.fromHex("0f0e0d0c0b0a09080706050403020100");
      const out = {};
      for (const mode of ["ecb", "cbc", "ctr", "gcm"]) {
        const enc = host.crypto.aes({ mode, key, iv: mode === "gcm" ? iv.slice(0, 12) : iv, data: "你好，一集", aad: mode === "gcm" ? "aad" : undefined });
        const dec = host.crypto.aes({ mode, key, iv: mode === "gcm" ? iv.slice(0, 12) : iv, data: enc, decrypt: true, aad: mode === "gcm" ? "aad" : undefined });
        out[mode] = { enc: hex(enc), dec: host.encoding.fromUtf8(dec), isU8: enc instanceof Uint8Array };
      }
      out.nopad = hex(host.crypto.aes({ mode: "ecb", key, data: new Uint8Array(16), padding: "none" }));
      try { host.crypto.aes({ mode: "ecb", key: "short", data: "x" }); } catch (e) { out.badKey = e.message; }
      return out;
    }
    case "rsa":
      return { enc: host.encoding.base64(host.crypto.rsa({ publicKey: a.key, data: "secret", padding: a.padding })) };
    case "random": {
      const r = host.crypto.randomBytes(32);
      let tooMany = "";
      try { host.crypto.randomBytes(2048); } catch (e) { tooMany = e.message; }
      return { len: r.length, uuid: host.crypto.uuid(), tooMany };
    }
    case "encoding":
      return {
        hex: hex(host.encoding.utf8("hi")),
        fromHex: host.encoding.fromUtf8(host.encoding.fromHex("6869")),
        b64: host.encoding.base64(new Uint8Array([251, 255, 191])),
        fromB64: hex(host.encoding.fromBase64("-_-_")),
        fromB64Std: hex(host.encoding.fromBase64("+/+/")),
        gunzip: host.encoding.fromUtf8(host.encoding.gunzip(host.encoding.fromBase64(a.gz))),
        inflate: host.encoding.fromUtf8(host.encoding.inflate(host.encoding.fromBase64(a.zlib))),
        inflateRaw: host.encoding.fromUtf8(host.encoding.inflate(host.encoding.fromBase64(a.raw), true)),
      };
    case "bomb":
      try { host.encoding.gunzip(host.encoding.fromBase64(a.gz)); return { ok: true }; } catch (e) { return { error: e.message }; }
    case "device":
      return host.device();
    case "storage": {
      const before = host.storage.get("k") ?? null;
      host.storage.set("k", "v" + (Number(host.storage.get("n") ?? "0") + 1));
      host.storage.set("n", String(Number(host.storage.get("n") ?? "0") + 1));
      host.storage.delete("gone");
      return { before, after: host.storage.get("k"), missing: host.storage.get("nope") === undefined };
    }
    case "cache": {
      const before = host.cache.get("c") ?? null;
      host.cache.set("c", "cached");
      let big = "";
      try { host.cache.set("big", "x".repeat(300 * 1024)); } catch (e) { big = e.message; }
      const first = host.cache.add("lock", "a", 60);
      const second = host.cache.add("lock", "b", 60);
      return { before, after: host.cache.get("c"), big, first, second, lock: host.cache.get("lock") };
    }
    case "web":
      return {
        url: new URL("/a?b=1", "https://example.com/x").href,
        params: new URLSearchParams({ q: "一集" }).toString(),
        text: new TextDecoder().decode(new TextEncoder().encode("编码")),
        b64: btoa("hi"),
        abort: typeof AbortController,
        headers: new Headers({ "X-A": "1" }).get("x-a"),
      };
    case "console":
      console.log("一行", 1, { a: 2 }, [3]);
      console.warn("警告");
      console.error(new Error("错误"));
      return { ok: true };
  }
  return {};
}
