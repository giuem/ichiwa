// 抛错：host.error 带错误码，其他异常一律算 plugin。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export async function collection(req) {
  switch (req.id) {
    case "rate": throw host.error("rateLimited", "慢一点", { retryAfter: 12 });
    case "gone": throw host.error("notFound", "没有了");
    case "unknown-code": throw host.error("teapot", "我是茶壶");
    case "fake": throw Object.assign(new Error("假装"), { code: "notFound" });
    case "string": throw "一个字符串";
    case "async": await Promise.resolve(); throw new TypeError("异步出错");
    case "never": return new Promise(() => {});
    case "number": return 42;
    case "timer": return new Promise((ok) => setTimeout(() => ok({ title: "late" }), 20));
  }
  return { title: "ok" };
}
