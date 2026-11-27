// 顶层就抛错的模块。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
export function collection() { return {}; }
throw new Error("顶层出错");
