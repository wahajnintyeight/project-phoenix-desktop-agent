import { el } from "./dom";
import { icon, type IconName } from "./icons";
import { send } from "./protocol";
import { mentionQuery } from "./mention-query";

type Item = { kind: "Task" | "File" | "Folder" | "MCP"; name: string; ref: string; detail?: string };
type Results = { seq: string; items: Item[] };
const glyph: Record<Item["kind"], IconName> = { Task: "check", File: "file", Folder: "folder", MCP: "plug" };

export class MentionPicker extends HTMLElement {
  #input!: HTMLTextAreaElement;
  #items: Item[] = [];
  #active = 0;
  #seq = 0;
  #timer = 0;
  #start = -1;
  #end = -1;
  #ignoreInput = false;

  attach(input: HTMLTextAreaElement): void {
    this.#input = input;
    this.id = "mention-picker";
    this.className = "slash-menu mention-menu";
    this.hidden = true;
    this.setAttribute("role", "listbox");
    this.setAttribute("aria-label", "Mention a task, file, folder or MCP server");
    input.addEventListener("input", () => this.#search());
    input.addEventListener("click", () => this.#search());
    input.addEventListener("blur", () => this.#close());
    input.addEventListener("keyup", (event) => { if (["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key)) this.#search(); });
    input.addEventListener("keydown", (event) => this.#key(event), true);
    this.addEventListener("mousedown", (event) => event.preventDefault());
    this.addEventListener("click", (event) => {
      const option = (event.target as Element).closest<HTMLButtonElement>("[data-mention-index]");
      if (option) this.#choose(Number(option.dataset["mentionIndex"]));
    });
  }

  set data(value: Results) {
    if (this.hidden || value.seq !== String(this.#seq)) return;
    this.#items = value.items;
    this.#active = 0;
    this.#render();
  }

  #search(): void {
    if (this.#ignoreInput) { this.#ignoreInput = false; return; }
    const end = this.#input.selectionStart;
    const match = mentionQuery(this.#input.value, end, this.#input.selectionEnd);
    clearTimeout(this.#timer);
    this.#seq++;
    if (!match) { this.#close(); return; }
    this.#start = match.start;
    this.#end = end;
    this.#items = [];
    this.hidden = false;
    this.#input.setAttribute("aria-controls", this.id);
    this.#input.setAttribute("aria-expanded", "true");
    this.replaceChildren(el("p", "slash-empty", "Searching…"));
    const seq = this.#seq;
    this.#timer = window.setTimeout(() => send("g", "mentions", match.query, String(seq)), 150);
  }

  #render(): void {
    this.replaceChildren(...this.#items.map((item, index) => {
      const button = el("button", "slash-option mention-option");
      button.type = "button";
      button.tabIndex = -1;
      button.id = `mention-option-${index}`;
      button.dataset["mentionIndex"] = String(index);
      button.setAttribute("role", "option");
      button.setAttribute("aria-selected", String(index === this.#active));
      const mark = el("span", "mention-icon");
      mark.append(icon(glyph[item.kind], 15));
      const label = el("span", "mention-label");
      label.append(el("span", "mention-name", item.name), el("span", "mention-detail", item.detail ?? item.kind));
      button.append(mark, label, el("span", "mention-kind", item.kind));
      return button;
    }));
    if (!this.#items.length) this.append(el("p", "slash-empty", "No matching tasks, files, folders or MCP servers."));
    this.#syncActive();
  }

  #syncActive(): void {
    this.querySelectorAll<HTMLButtonElement>("[data-mention-index]").forEach((option, index) => {
      const active = index === this.#active;
      option.setAttribute("aria-selected", String(active));
      if (active) { this.#input.setAttribute("aria-activedescendant", option.id); option.scrollIntoView({ block: "nearest" }); }
    });
    if (!this.#items.length) this.#input.removeAttribute("aria-activedescendant");
  }

  #key(event: KeyboardEvent): void {
    if (this.hidden || event.isComposing) return;
    if (event.key === "Escape") { event.preventDefault(); event.stopImmediatePropagation(); this.#close(); return; }
    if ((event.key === "ArrowDown" || event.key === "ArrowUp") && this.#items.length) {
      event.preventDefault(); event.stopImmediatePropagation();
      this.#active = (this.#active + (event.key === "ArrowDown" ? 1 : -1) + this.#items.length) % this.#items.length;
      this.#syncActive();
    } else if ((event.key === "Enter" || event.key === "Tab") && this.#items.length && !event.shiftKey) {
      event.preventDefault(); event.stopImmediatePropagation(); this.#choose(this.#active);
    }
  }

  #choose(index: number): void {
    const item = this.#items[index];
    if (!item) return;
    const value = item.kind === "Task" ? `${item.name} (id: ${item.ref})` : item.ref;
    const text = `@${item.kind}:${JSON.stringify(value)} `;
    this.#input.setRangeText(text, this.#start, this.#end, "end");
    this.#close();
    this.#ignoreInput = true;
    this.#input.dispatchEvent(new Event("input", { bubbles: true }));
    this.#input.focus();
  }

  #close(): void {
    clearTimeout(this.#timer);
    this.#seq++;
    this.hidden = true;
    this.#items = [];
    this.#input.setAttribute("aria-controls", "slash-commands");
    const slash = document.getElementById("slash-commands");
    this.#input.setAttribute("aria-expanded", String(!!slash && !slash.hidden));
    if (!slash || slash.hidden) this.#input.removeAttribute("aria-activedescendant");
  }
}
customElements.define("ph-mentions", MentionPicker);
