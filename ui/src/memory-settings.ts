// <ph-memory-settings>: everything Phoenix has learned about the user, grouped by topic, with
// search, add, edit and delete. Go owns the data; this element only displays it. Text is only
// ever placed with .value or textContent.
import { el } from "./dom";
import { send } from "./protocol";

interface Entry {
  id: number;
  kind: string;
  topic: string;
  key: string;
  value: string;
  uses: number;
  updated: number;
}

interface Outcome {
  ok?: boolean;
  text?: string;
  error?: string;
}

const KIND_LABEL: Record<string, string> = {
  fact: "Detail",
  answer: "Answer",
  preference: "Preference",
  choice: "Your choice",
};

export class PhMemorySettings extends HTMLElement {
  #entries: Entry[] = [];
  #editing = 0;
  #confirmClear = false;
  readonly #search = el("input", "field");
  readonly #count = el("p", "text-xs text-fg-muted");
  readonly #list = el("div", "flex flex-col gap-4");
  readonly #result = el("p", "msg");
  readonly #clear = el("button", "btn", "Forget everything");
  readonly #topicIn = el("input", "field");
  readonly #keyIn = el("input", "field");
  readonly #valueIn = el("input", "field field-wide");
  readonly #kindIn = el("select", "field");
  readonly #addBtn = el("button", "btn btn-primary", "Remember");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#search.type = "search";
    this.#search.placeholder = "Search memory";
    this.#search.setAttribute("aria-label", "Search memory");
    this.#clear.type = "button";
    this.#result.setAttribute("aria-live", "polite");

    this.#topicIn.placeholder = "Section (for example Contact)";
    this.#topicIn.setAttribute("aria-label", "Section");
    this.#keyIn.placeholder = "Question or label (optional)";
    this.#keyIn.setAttribute("aria-label", "Question or label");
    this.#valueIn.placeholder = "What to remember";
    this.#valueIn.setAttribute("aria-label", "What to remember");
    for (const [v, l] of [["fact", "Detail"], ["answer", "Answer to a question"], ["preference", "Preference"]] as const) {
      const o = el("option", undefined, l);
      o.value = v;
      this.#kindIn.append(o);
    }
    this.#kindIn.setAttribute("aria-label", "Type");
    this.#addBtn.type = "button";
    const add = el("div", "mem-add");
    add.append(this.#topicIn, this.#kindIn, this.#keyIn, this.#valueIn, this.#addBtn);
    this.#keyIn.className = "field field-wide";

    const bar = el("div", "mem-bar");
    bar.append(this.#search, this.#clear);
    this.append(bar, this.#count, add, this.#result, this.#list);

    this.#search.addEventListener("input", () => this.#render(), { passive: true });
    this.#addBtn.addEventListener("click", () => {
      const value = this.#valueIn.value.trim();
      if (!value) return;
      send("v", "memory-save", "0", this.#kindIn.value, this.#topicIn.value, this.#keyIn.value, value);
      this.#valueIn.value = "";
      this.#keyIn.value = "";
    }, { passive: true });
    this.#valueIn.addEventListener("keydown", (e) => { if (e.key === "Enter") this.#addBtn.click(); });
    this.#clear.addEventListener("click", () => {
      if (!this.#confirmClear) {
        this.#confirmClear = true;
        this.#clear.textContent = "Click again to forget everything";
        setTimeout(() => { this.#confirmClear = false; this.#clear.textContent = "Forget everything"; }, 4000);
        return;
      }
      this.#confirmClear = false;
      this.#clear.textContent = "Forget everything";
      send("v", "memory-clear");
    }, { passive: true });
    this.#render();
  }

  set data(v: { entries: Entry[] }) {
    this.#entries = v.entries ?? [];
    this.#editing = 0;
    this.#render();
  }

  set result(v: Outcome) {
    this.#result.className = `msg ${v.error ? "is-error" : "is-ok"}`;
    this.#result.textContent = v.error ?? v.text ?? "";
  }

  #render(): void {
    const words = this.#search.value.toLowerCase().split(/\s+/).filter(Boolean);
    const shown = this.#entries.filter((e) => {
      const hay = `${e.topic} ${e.key} ${e.value}`.toLowerCase();
      return words.every((w) => hay.includes(w));
    });
    this.#clear.hidden = this.#entries.length === 0;
    this.#count.textContent = this.#entries.length === 0
      ? "Nothing yet. Phoenix remembers what you tell the agent and what you choose; you can also add things here."
      : `${shown.length} of ${this.#entries.length} remembered. Stored only on this computer; a short version goes with every message.`;

    this.#list.replaceChildren();
    const byTopic = new Map<string, Entry[]>();
    for (const e of shown) byTopic.set(e.topic, [...(byTopic.get(e.topic) ?? []), e]);
    for (const [topic, rows] of byTopic) {
      const sec = el("section", "mem-topic");
      sec.append(el("h3", undefined, topic));
      for (const e of rows) sec.append(e.id === this.#editing ? this.#editRow(e) : this.#row(e));
      this.#list.append(sec);
    }
  }

  #row(e: Entry): HTMLElement {
    const row = el("div", "mem-row");
    const text = el("div", "mem-text");
    if (e.key) text.append(el("span", "mem-key", e.key));
    text.append(el("span", "mem-value", e.value));
    const used = e.uses > 1 ? ` · seen ${e.uses} times` : "";
    text.append(el("span", "mem-meta", (KIND_LABEL[e.kind] ?? e.kind) + used));
    const actions = el("div", "mem-actions");
    const edit = el("button", "btn btn-sm", "Edit");
    const del = el("button", "btn btn-sm", "Forget");
    edit.type = del.type = "button";
    edit.setAttribute("aria-label", `Edit ${e.key || e.value}`);
    del.setAttribute("aria-label", `Forget ${e.key || e.value}`);
    edit.addEventListener("click", () => { this.#editing = e.id; this.#render(); }, { passive: true });
    del.addEventListener("click", () => send("v", "memory-delete", String(e.id)), { passive: true });
    actions.append(edit, del);
    row.append(text, actions);
    return row;
  }

  #editRow(e: Entry): HTMLElement {
    const row = el("div", "mem-row");
    const form = el("div", "mem-edit");
    const topic = el("input", "field");
    const key = el("input", "field");
    const value = el("input", "field");
    topic.value = e.topic;
    key.value = e.key;
    value.value = e.value;
    topic.setAttribute("aria-label", "Section");
    key.setAttribute("aria-label", "Question or label");
    value.setAttribute("aria-label", "Value");
    const save = el("button", "btn btn-sm btn-primary", "Save");
    const cancel = el("button", "btn btn-sm", "Cancel");
    save.type = cancel.type = "button";
    save.addEventListener("click", () => send("v", "memory-save", String(e.id), e.kind, topic.value, key.value, value.value), { passive: true });
    cancel.addEventListener("click", () => { this.#editing = 0; this.#render(); }, { passive: true });
    value.addEventListener("keydown", (ev) => { if (ev.key === "Enter") save.click(); });
    const actions = el("div", "mem-actions");
    actions.append(save, cancel);
    form.append(topic, key, value);
    row.append(form, actions);
    queueMicrotask(() => value.focus());
    return row;
  }
}

customElements.define("ph-memory-settings", PhMemorySettings);
