// Run: node ui/src/choices.test.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import ts from "typescript";

class Element {
  constructor(tag, className, text = "") {
    Object.assign(this, { tag, className, textContent: text, children: [], attrs: {}, handlers: {}, value: "", disabled: false });
  }
  append(...children) { for (const child of children) { child.parent = this; this.children.push(child); } }
  insertBefore(child, reference) { child.parent = this; this.children.splice(this.children.indexOf(reference), 0, child); }
  remove() { if (this.parent) this.parent.children.splice(this.parent.children.indexOf(this), 1); }
  setAttribute(key, value) { this.attrs[key] = value; }
  getAttribute(key) { return this.attrs[key]; }
  addEventListener(name, listener) { this.handlers[name] = listener; }
  fire(name) { this.handlers[name]?.({ preventDefault() {} }); }
}

const source = await readFile(new URL("./choices.ts", import.meta.url), "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const sandbox = { exports: {}, require: (name) => name === "./dom" ? { el: (tag, cls, text) => new Element(tag, cls, text) } : {} };
vm.runInNewContext(compiled, sandbox);
const { ChoiceMenus } = sandbox.exports;
const first = { question: "Which board?", options: ["Indeed", "LinkedIn"], kind: "question" };
const second = { question: "CV?", options: ["Upload", "In files"], kind: "question" };
let answers;
const menus = new ChoiceMenus((value) => { answers = value; });
menus.setMode("streaming");
menus.update([first]);
const group = menus.element.children[0];
const indeed = group.children[2].children[0];
indeed.fire("click");
assert.equal(indeed.getAttribute("aria-pressed"), "true");
assert.equal(menus.element.children.at(-1).disabled, true);
menus.update([first, second]);
assert.equal(menus.element.children[0], group, "streaming another question must preserve the first answer");
const input = menus.element.children[1].children[1];
input.value = "Saved in files";
input.fire("input");
menus.setMode("ready");
assert.equal(menus.element.children.at(-1).disabled, false);
menus.element.fire("submit");
assert.deepEqual(JSON.parse(JSON.stringify(answers)), [{ question: "Which board?", answer: "Indeed", kind: "question" }, { question: "CV?", answer: "Saved in files", kind: "question" }]);
assert.equal(indeed.disabled, true);

let single;
const one = new ChoiceMenus((value) => { single = value; });
one.update([first]);
one.setMode("ready");
one.element.children[0].children[2].children[1].fire("click");
assert.deepEqual(JSON.parse(JSON.stringify(single)), [{ question: "Which board?", answer: "LinkedIn", kind: "question" }]);
let action;
const actions = new ChoiceMenus((value) => { action = value; });
actions.update([{ question: "Which company?", options: ["Explore Nextcloud", "Explore PROBIS"], kind: "action" }]);
actions.setMode("ready");
actions.element.children[0].children[2].children[0].fire("click");
assert.deepEqual(JSON.parse(JSON.stringify(action)), [{ question: "Which company?", answer: "Explore Nextcloud", kind: "action" }]);
console.log("Question menu streaming and submission checks passed.");
