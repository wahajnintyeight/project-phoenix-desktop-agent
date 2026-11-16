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

interface Lesson {
  id: number;
  kind: string;
  trigger: string;
  lesson: string;
  evidence: string;
  sourceChat?: string | undefined;
  scope?: string | undefined;
  supersedes?: number | undefined;
  pinned?: boolean | undefined;
  status: "candidate" | "active" | "superseded";
  helpful: number;
  unhelpful: number;
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
const MEMORY_PAGE_SIZE = 8;

export class PhMemorySettings extends HTMLElement {
  #entries: Entry[] = [];
  #lessons: Lesson[] = [];
  #learningEnabled = false;
  #editingLesson = 0;
  #editing = 0;
  #visibleEntries = MEMORY_PAGE_SIZE;
  #confirmClear = false;
  readonly #search = el("input", "field");
  readonly #count = el("p", "text-xs text-fg-muted");
  readonly #list = el("div", "flex flex-col gap-4");
  readonly #result = el("p", "msg");
  readonly #clear = el("button", "btn", "Clear memory");
  readonly #topicIn = el("input", "field");
  readonly #keyIn = el("input", "field");
  readonly #valueIn = el("input", "field field-wide");
  readonly #kindIn = el("select", "field");
  readonly #addBtn = el("button", "btn btn-primary w-fit", "Remember");
  readonly #learning = el("input");
  readonly #budget = el("input", "field");
  readonly #lessonList = el("div", "flex flex-col gap-4");

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
    this.#learning.type = "checkbox";
    this.#learning.setAttribute("aria-label", "Learn from completed work");
    const learningLabel = el("label", "tp-check");
    learningLabel.append(this.#learning, el("span", "tp-check-text", "Suggest lessons from completed work"));
    const learningInfo = el("p", "text-xs text-fg-muted", "Review suggestions before use. Evidence is reported by the agent and may be wrong; some corrections may be missed. Lessons are selected by matching words and scope. Pin up to two short, unscoped approved lessons for every request. Turning this off stops new suggestions; saved memories still work.");
    this.#budget.type = "number"; this.#budget.min = "400"; this.#budget.max = "3200"; this.#budget.step = "100";
    this.#budget.setAttribute("aria-label", "Maximum retrieved memory bytes");
    const budgetLabel = el("label", "mem-text"); budgetLabel.append(el("span", "font-medium", "Memory context limit (bytes)"), this.#budget, el("span", "text-xs text-fg-muted", "Shared by remembered facts and lessons; bytes are not model tokens."));
    const saveBudget = el("button", "btn btn-sm w-fit", "Save memory limit"); saveBudget.type = "button";
    saveBudget.addEventListener("click", () => send("v", "memory-budget", this.#budget.value));
    this.append(bar, this.#count, learningLabel, learningInfo, budgetLabel, saveBudget, add, this.#result, this.#lessonList, this.#list);

    this.#learning.addEventListener("change", () => send("v", "memory-learning", this.#learning.checked ? "1" : "0"), { passive: true });

    this.#search.addEventListener("input", () => { this.#visibleEntries = MEMORY_PAGE_SIZE; this.#render(); }, { passive: true });
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
        this.#clear.textContent = "Click again to clear memory";
        setTimeout(() => { this.#confirmClear = false; this.#clear.textContent = "Clear memory"; }, 4000);
        return;
      }
      this.#confirmClear = false;
      this.#clear.textContent = "Clear memory";
      send("v", "memory-clear");
    }, { passive: true });
    this.#render();
  }

  set data(v: { entries: Entry[]; lessons?: Lesson[]; learningEnabled?: boolean; budget?: number }) {
    this.#entries = v.entries ?? [];
    this.#lessons = v.lessons ?? [];
    this.#learningEnabled = v.learningEnabled ?? false;
    this.#learning.checked = this.#learningEnabled;
    this.#budget.value = String(v.budget ?? 1600);
    this.#editing = 0;
    this.#editingLesson = 0;
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
    this.#clear.hidden = this.#entries.length === 0 && this.#lessons.length === 0;
    const visible = shown.slice(0, this.#visibleEntries);
    const displayCount = shown.length > MEMORY_PAGE_SIZE ? ` · ${visible.length} shown` : "";
    this.#count.textContent = this.#entries.length === 0 && this.#lessons.length === 0
      ? "Nothing yet. Phoenix remembers what you tell the agent and what you choose; you can also add things here."
      : `${shown.length} of ${this.#entries.length} memories${displayCount} · ${this.#lessons.length} lessons. Stored on this computer and used when relevant.`;

    this.#lessonList.replaceChildren();
    const lessons = this.#lessons.filter((l) => words.every((w) => `${l.trigger} ${l.lesson} ${l.evidence}`.toLowerCase().includes(w)));
    if (lessons.length) {
      const section = el("section", "mem-topic");
      section.append(el("h3", undefined, "Learned lessons"));
      for (const lesson of lessons) section.append(this.#lessonRow(lesson));
      this.#lessonList.append(section);
    }

    this.#list.replaceChildren();
    const byTopic = new Map<string, Entry[]>();
    for (const e of visible) byTopic.set(e.topic, [...(byTopic.get(e.topic) ?? []), e]);
    for (const [topic, rows] of byTopic) {
      const sec = el("section", "mem-topic");
      sec.append(el("h3", undefined, topic));
      for (const e of rows) sec.append(e.id === this.#editing ? this.#editRow(e) : this.#row(e));
      this.#list.append(sec);
    }
    if (shown.length > visible.length) {
      const remaining = shown.length - visible.length;
      const more = el("button", "btn btn-sm btn-quiet w-fit self-start", `Show ${Math.min(MEMORY_PAGE_SIZE, remaining)} more memories · ${remaining} remaining`);
      more.type = "button";
      more.addEventListener("click", () => { this.#visibleEntries += MEMORY_PAGE_SIZE; this.#render(); }, { passive: true });
      this.#list.append(more);
    } else if (this.#visibleEntries > MEMORY_PAGE_SIZE) {
      const fewer = el("button", "btn btn-sm btn-quiet w-fit self-start", "Show fewer memories");
      fewer.type = "button";
      fewer.addEventListener("click", () => { this.#visibleEntries = MEMORY_PAGE_SIZE; this.#render(); }, { passive: true });
      this.#list.append(fewer);
    }
  }

  #lessonRow(item: Lesson): HTMLElement {
    const row = el("div", "mem-row");
    const body = el("div", "mem-text");
    const actions = el("div", "mem-actions");
    const act = (label: string, action: string): HTMLButtonElement => {
      const button = el("button", "btn btn-sm", label);
      button.type = "button";
      button.addEventListener("click", () => send("v", "memory-lesson", String(item.id), action), { passive: true });
      return button;
    };
    if (this.#editingLesson === item.id) {
      const trigger = el("input", "field");
      const lesson = el("textarea", "field");
      const scope = el("input", "field");
      trigger.value = item.trigger;
      lesson.value = item.lesson;
      scope.value = item.scope ?? "";
      scope.placeholder = "Scope (optional)";
      scope.setAttribute("aria-label", "Lesson scope");
      trigger.setAttribute("aria-label", "When this lesson is relevant");
      lesson.setAttribute("aria-label", "Lesson");
      const save = el("button", "btn btn-sm btn-primary", "Save");
      save.type = "button";
      save.addEventListener("click", () => send("v", "memory-lesson", String(item.id), "edit", trigger.value, lesson.value, scope.value), { passive: true });
      const cancel = el("button", "btn btn-sm", "Cancel");
      cancel.type = "button";
      cancel.addEventListener("click", () => { this.#editingLesson = 0; this.#render(); }, { passive: true });
      body.append(trigger, lesson, scope);
      actions.append(save, cancel);
    } else {
      body.append(el("span", "mem-key", item.trigger), el("span", "mem-value", item.lesson));
      body.append(el("span", "mem-meta", `${item.status === "candidate" ? "Suggested · review before use" : item.status === "superseded" ? "Superseded" : `Approved · ${item.helpful} helpful · ${item.unhelpful} needs work`} · ${item.kind}${item.scope ? ` · ${item.scope}` : ""}${item.pinned ? " · Pinned for every request" : ""} · Agent suggestion`));
      body.append(el("span", "mem-meta", `Agent-reported evidence (not independently verified): ${item.evidence}`));
      if (item.supersedes) body.append(el("span", "mem-meta", `Replaces lesson #${item.supersedes}`));
      if (item.sourceChat) {
        const source = el("button", "btn btn-sm", "Open source chat");
        source.type = "button";
        source.addEventListener("click", () => send("k", "session", item.sourceChat ?? ""), { passive: true });
        actions.append(source);
      }
      if (item.status === "candidate") {
        actions.append(act("Approve", "approve"));
        const words = new Set(item.trigger.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((word) => word.length >= 3));
        const active = this.#lessons.filter((other) => other.status === "active").sort((a, b) => {
          const score = (text: string): number => text.toLowerCase().split(/[^\p{L}\p{N}]+/u).filter((word) => words.has(word)).length;
          return score(b.trigger) - score(a.trigger);
        });
        if (active.length) {
          const select = el("select", "field");
          select.setAttribute("aria-label", "Choose an approved lesson to replace");
          const empty = el("option", undefined, "Choose an approved lesson to replace…"); empty.value = ""; select.append(empty);
          body.append(el("span", "mem-meta", "Related triggers appear first. Compare the lessons before replacing one."));
          for (const other of active) { const option = el("option", undefined, `${other.trigger} — ${other.lesson}`); option.value = String(other.id); select.append(option); }
          const replace = el("button", "btn btn-sm", "Approve replacement"); replace.type = "button";
          replace.addEventListener("click", () => { if (select.value) send("v", "memory-lesson", String(item.id), "replace", select.value); });
          body.append(select); actions.append(replace);
        }
      } else if (item.status === "active") actions.append(act("Helpful", "helpful"), act("Needs work", "unhelpful"), act(item.pinned ? "Unpin" : "Pin", item.pinned ? "unpin" : "pin"));
      const edit = el("button", "btn btn-sm", "Edit");
      edit.type = "button";
      edit.addEventListener("click", () => { this.#editingLesson = item.id; this.#render(); }, { passive: true });
      actions.append(edit, act("Forget", "delete"));
    }
    row.append(body, actions);
    return row;
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
