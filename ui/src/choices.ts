import { el } from "./dom";
import type { ChoiceSet } from "./choice-parser";

export { splitChoices } from "./choice-parser";
export type { ChoiceSet } from "./choice-parser";
export interface ChoiceAnswer { question: string; answer: string; kind: ChoiceSet["kind"] }
export type ChoiceMode = "inactive" | "streaming" | "ready" | "submitted";
type Group = { key: string; set: ChoiceSet; node: HTMLElement; input: HTMLInputElement; buttons: HTMLButtonElement[]; answer: string };

/** A response can add question groups while streaming without erasing earlier selections. */
export class ChoiceMenus {
  readonly element = el("form", "chat-question-menus");
  readonly #status = el("p", "chat-question-status");
  readonly #submit = el("button", "btn btn-primary btn-sm chat-question-submit", "Continue");
  #groups: Group[] = [];
  #mode: ChoiceMode = "inactive";

  constructor(private readonly answer: (answers: ChoiceAnswer[]) => void) {
    this.element.setAttribute("aria-label", "Answer the agent's questions");
    this.#status.setAttribute("role", "status");
    this.#submit.type = "submit";
    this.element.append(this.#status, this.#submit);
    this.element.addEventListener("submit", (event) => { event.preventDefault(); this.#send(); });
  }

  update(sets: ChoiceSet[]): void {
    let keep = 0;
    while (keep < sets.length && keep < this.#groups.length && this.#groups[keep]?.key === JSON.stringify(sets[keep])) keep++;
    for (const group of this.#groups.splice(keep)) group.node.remove();
    for (let i = keep; i < sets.length; i++) {
      const set = sets[i];
      if (!set) continue;
      const group = this.#group(set);
      this.#groups.push(group);
      this.element.insertBefore(group.node, this.#status);
    }
    this.#refresh();
  }

  setMode(mode: ChoiceMode): void { this.#mode = mode; this.#refresh(); }

  #group(set: ChoiceSet): Group {
    const node = el("fieldset", "chat-question");
    node.append(el("legend", "chat-question-label", set.question));
    const input = el("input", "chat-question-input");
    input.type = "text";
    input.maxLength = 500;
    input.placeholder = "Type your own answer…";
    input.setAttribute("aria-label", `Your answer: ${set.question}`);
    const list = el("div", "chat-question-options");
    const group: Group = { key: JSON.stringify(set), set, node, input, buttons: [], answer: "" };
    input.addEventListener("input", () => {
      group.answer = input.value.trim();
      group.buttons.forEach((button) => button.setAttribute("aria-pressed", "false"));
      this.#refresh();
    });
    for (const option of set.options) {
      const button = el("button", "btn chat-question-option", option);
      button.type = "button";
      button.setAttribute("aria-pressed", "false");
      button.addEventListener("click", () => {
        group.answer = option;
        input.value = "";
        group.buttons.forEach((other) => other.setAttribute("aria-pressed", String(other === button)));
        this.#refresh();
        if (this.#groups.length === 1 && this.#mode === "ready") this.#send();
      });
      group.buttons.push(button);
      list.append(button);
    }
    node.append(input, list);
    return group;
  }

  #refresh(): void {
    const editable = this.#mode === "streaming" || this.#mode === "ready";
    const status = this.#mode === "streaming" ? "Waiting for the agent to finish…" : "";
    if (this.#status.textContent !== status) this.#status.textContent = status;
    for (const group of this.#groups) {
      group.input.disabled = !editable;
      group.buttons.forEach((button) => { button.disabled = !editable; });
    }
    this.#submit.disabled = this.#mode !== "ready" || !this.#groups.length || this.#groups.some((group) => !group.answer);
    this.#submit.textContent = this.#groups.length === 1 ? "Send answer" : "Continue";
  }

  #send(): void {
    if (this.#submit.disabled) return;
    const answers = this.#groups.map((group) => ({ question: group.set.question, answer: group.answer, kind: group.set.kind }));
    this.setMode("submitted");
    this.answer(answers);
  }
}
