// 死循环：超时后被宿主中断；或者一直打空行。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection(req) {
  if (req.id === "spam") for (;;) console.log();
  for (;;) {}
}
