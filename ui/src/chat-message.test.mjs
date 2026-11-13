// Run: node ui/src/chat-message.test.mjs
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import ts from "typescript";

class Element {
  constructor(tag, className = "", textContent = "") {
    Object.assign(this, { tag, className, textContent, children: [], attrs: {}, isConnected: false, parentElement: null });
    this.classList = { contains: (name) => this.className.split(" ").includes(name) };
  }
  append(...children) { for (const child of children) { child.remove(); child.parent = this; child.parentElement = this; this.children.push(child); } }
  after(child) { child.remove(); child.parent = this.parent; child.parentElement = this.parent; this.parent.children.splice(this.parent.children.indexOf(this) + 1, 0, child); }
  remove() { if (this.parent) this.parent.children.splice(this.parent.children.indexOf(this), 1); this.parent = null; this.parentElement = null; }
  setAttribute(key, value) { this.attrs[key] = value; }
  removeAttribute(key) { delete this.attrs[key]; }
  addEventListener() {}
}

const compile = (source) => ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const parser = { exports: {} };
vm.runInNewContext(compile(await readFile(new URL("./choice-parser.ts", import.meta.url), "utf8")), parser);
class ChoiceMenus {
  element = new Element("form", "choice-menu");
  update() {}
  setMode() {}
}
const modules = {
  "./choices": { ChoiceMenus, splitChoices: parser.exports.splitChoices },
  "./dom": { el: (tag, cls, text) => new Element(tag, cls, text) },
  "./icons": { icon: () => new Element("svg") },
  "./markdown": { MarkdownView: class { set() {} } },
};
const sandbox = { exports: {}, require: (name) => modules[name], document: { createElement: (tag) => new Element(tag) }, requestAnimationFrame: () => 1, cancelAnimationFrame() {} };
vm.runInNewContext(compile(await readFile(new URL("./chat-message.ts", import.meta.url), "utf8")), sandbox);
const { Msg } = sandbox.exports;
const inline = "Result.\n```actions\nWhich role?\n- Review Joppy\n- Review LHH\n```";
const menus = (msg) => msg.box.children.filter((child) => child.className === "choice-menu").length;

const reply = new Msg("Agent", "chat-ai", true, 0);
reply.append(inline);
reply.flush();
reply.setSuggestions(["Search again", "Change filters"]);
assert.equal(menus(reply), 1, "inline choices and generated suggestions must not create two menus");

const later = new Msg("Agent", "chat-ai", true, 0);
later.append("Result.");
later.flush();
later.setSuggestions(["Search again", "Change filters"]);
later.append(inline);
later.flush();
assert.equal(menus(later), 1, "an inline menu arriving later must replace generated suggestions");

const plain = new Msg("Agent", "chat-ai", true, 0);
plain.append("Result.");
plain.flush();
plain.setSuggestions(["Search again", "Change filters"]);
plain.setSuggestions(["Search again", "Change filters"]);
assert.equal(menus(plain), 1, "repeated suggestion events must reuse the same menu");
const vault = new Element("div", "chat-choice-vault");
plain.mountChoices(vault);
assert.equal(vault.children.length, 1, "the active menu must mount in the composer vault");
assert.equal(menus(plain), 0, "vault menus must leave the transcript message");
console.log("Chat message one-menu checks passed.");
