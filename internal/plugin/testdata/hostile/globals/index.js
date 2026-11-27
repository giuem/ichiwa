// 想在调用之间靠全局变量留状态：每次调用都是新的运行时，留不住。
export function navigation() { return { items: [{ id: "x", title: "x" }] }; }
export function media() { return {}; }
export function play() { return {}; }
let n = 0;
export function collection() { n++; globalThis.m = (globalThis.m ?? 0) + 1; return { title: n + "," + globalThis.m }; }
