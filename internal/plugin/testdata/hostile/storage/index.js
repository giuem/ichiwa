// host.storage 超额：存 70 KiB。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection(req) {
  if (req.id === "catch") {
    try { host.storage.set("big", "x".repeat(70 * 1024)); } catch (e) { return { title: "caught: " + e.message }; }
    return { title: "stored" };
  }
  host.storage.set("big", "x".repeat(70 * 1024));
  return { title: "stored" };
}
