import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";
import ts from "typescript";

class Element {
  constructor() { this.dataset = {}; this.attrs = {}; this.children = []; this.listeners = {}; }
  setAttribute(key, value) { this.attrs[key] = value; }
  append(...children) { this.children.push(...children); }
  replaceChildren(...children) { this.children = children; }
  addEventListener(name, fn) { this.listeners[name] = fn; }
  click() { this.listeners.click(); }
}
const source = await readFile(new URL("./chat-choice-vault.ts", import.meta.url), "utf8");
const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText;
const sandbox = { exports: {}, require: (name) => name === "./dom" ? { el: () => new Element() } : { icon: () => new Element() } };
vm.runInNewContext(code, sandbox);
const vault = new sandbox.exports.ChatChoiceVault();
const form = new Element();
form.answer = "Yes";
let dismissed = false;
vault.show({ mountChoices: (host) => host.append(form), dismissChoices: () => { dismissed = true; } });
const [toggle, close] = vault.element.children;
toggle.click();
assert.equal(vault.element.dataset.collapsed, "true");
assert.equal(toggle.attrs["aria-expanded"], "false");
toggle.click();
assert.equal(vault.element.dataset.collapsed, "false");
assert.equal(vault.element.children[2], form);
assert.equal(form.answer, "Yes");
assert.equal(dismissed, false);
close.click();
assert.equal(dismissed, true);
assert.equal(vault.element.children.length, 0);
console.log("Choice panel collapse checks passed.");
