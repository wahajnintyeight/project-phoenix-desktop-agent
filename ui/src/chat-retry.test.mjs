import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import ts from "typescript";

class Element {
  constructor(tag = "div", className = "", textContent = "") {
    Object.assign(this, { tag, className, textContent, children: [], dataset: {}, value: "", hidden: false, disabled: false, clientHeight: 0 });
    this.classList = { add() {}, remove() {} };
  }
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  replaceChildren(...children) { this.children = []; this.append(...children); }
  remove() { if (this.parent) this.parent.children = this.parent.children.filter((child) => child !== this); }
  addEventListener(name, handler) { this[name] = handler; }
  setAttribute() {}
  removeAttribute() {}
  querySelectorAll(selector) { return this.children.flatMap((child) => [...(selector === ".chat-retry" && child.className.includes("chat-retry") ? [child] : []), ...child.querySelectorAll(selector)]); }
}
const sent = [];
const created = [];
const modules = {
  "./dom": { el: (tag, cls, text) => { const element = new Element(tag, cls, text); created.push(element); return element; } },
  "./icons": { icon: () => new Element() },
  "./protocol": { send: (...args) => sent.push(args) },
  "./chat-message": { Msg: class { constructor() { this.box = new Element(); this.meta = new Element(); } set() {} } },
  "./chat-choice-vault": { ChatChoiceVault: class { show() {} } },
  "./compact-reply": { CompactReply: class { setStreaming() {} clear() {} } },
  "./voice": { createVoiceButton: () => new Element() },
};
const sandbox = { exports: {}, require: (name) => modules[name] ?? {}, HTMLElement: Element, ResizeObserver: class {}, document: {}, customElements: { define() {} }, requestAnimationFrame: () => 1 };
const source = await readFile(new URL("./chat.ts", import.meta.url), "utf8");
vm.runInNewContext(ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText, sandbox);
const chat = new sandbox.exports.PhChat();
chat.events = [{ k: "error", text: "failed" }];
const retry = created.find((element) => element.className.includes("chat-retry"));
assert.ok(retry);
assert.equal(retry.disabled, false);
retry.click();
assert.deepEqual(sent, [["g", "send", "retry", "0"]]);
assert.equal(retry.disabled, true);
retry.click();
assert.equal(sent.length, 1, "a running retry must not send twice");
chat.events = [{ k: "user", text: "retry" }];
assert.ok(!retry.parent.children.includes(retry), "an older retry button must be removed");
console.log("Chat retry checks passed.");

