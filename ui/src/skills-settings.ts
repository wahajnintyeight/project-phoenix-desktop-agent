import { el } from "./dom";
import { send } from "./protocol";

interface Skill {
  id: number;
  name: string;
  description: string;
  instructions: string;
  enabled: boolean;
}

interface Outcome { ok?: boolean; text?: string; error?: string }

export class PhSkillsSettings extends HTMLElement {
  #items: Skill[] = [];
  #editing = 0;
  readonly #name = el("input", "field");
  readonly #description = el("input", "field");
  readonly #instructions = el("textarea", "field field-wide");
  readonly #save = el("button", "btn btn-primary w-fit", "Save skill");
  readonly #cancel = el("button", "btn w-fit", "Cancel");
  readonly #list = el("div", "flex flex-col gap-3");
  readonly #result = el("p", "msg");
  readonly #formTitle = el("h3", undefined, "Create a skill");

  connectedCallback(): void {
    if (this.childElementCount) return;
    this.#name.placeholder = "Skill name";
    this.#name.maxLength = 80;
    this.#name.setAttribute("aria-label", "Skill name");
    this.#description.placeholder = "Use when… (for example, researching job listings)";
    this.#description.maxLength = 300;
    this.#description.setAttribute("aria-label", "When to use this skill");
    this.#instructions.placeholder = "Instructions for the agent";
    this.#instructions.maxLength = 3000;
    this.#instructions.rows = 6;
    this.#instructions.setAttribute("aria-label", "Skill instructions");
    this.#result.setAttribute("aria-live", "polite");
    this.#save.type = "button";
    this.#cancel.type = "button";
    this.#cancel.hidden = true;
    const form = el("section", "skills-form");
    const intro = el("div", "skills-form-intro");
    intro.append(this.#formTitle, el("p", "text-fg-muted", "Describe when to use it and the steps Phoenix should follow."));
    const field = (label: string, input: HTMLElement, hint?: string): HTMLElement => {
      const wrapper = el("label", "skills-field");
      wrapper.append(el("span", undefined, label), input);
      if (hint) wrapper.append(el("span", "skills-hint", hint));
      return wrapper;
    };
    form.append(intro, field("Name", this.#name), field("When to use", this.#description, "Include words you would use when asking for this task."), field("Instructions", this.#instructions, "Keep instructions concise. Maximum 3,000 bytes."));
    const actions = el("div", "skills-actions");
    actions.append(this.#save, this.#cancel);
    form.append(actions);
    this.append(form, this.#result, this.#list);
    this.#save.addEventListener("click", () => {
      send("v", "skills-save", String(this.#editing), this.#name.value, this.#description.value, this.#instructions.value);
    });
    this.#cancel.addEventListener("click", () => this.#reset());
    this.#render();
  }

  set data(items: Skill[]) {
    this.#items = Array.isArray(items) ? items : [];
    this.#render();
  }

  set result(value: Outcome) {
    this.#result.className = `msg ${value.error ? "is-error" : "is-ok"}`;
    this.#result.textContent = value.error ?? value.text ?? "";
    if (value.ok) this.#reset();
  }

  #reset(): void {
    this.#editing = 0;
    this.#name.value = "";
    this.#description.value = "";
    this.#instructions.value = "";
    this.#save.textContent = "Save skill";
    this.#formTitle.textContent = "Create a skill";
    this.#cancel.hidden = true;
  }

  #render(): void {
    if (!this.#list) return;
    this.#list.replaceChildren();
    if (!this.#items.length) {
      const empty = el("div", "skills-empty");
      empty.append(el("h3", undefined, "Your skills will appear here"), el("p", "text-fg-muted", "Create a playbook above for a task you repeat."));
      this.#list.append(empty);
      return;
    }
    this.#list.append(el("h3", "skills-list-heading", `Saved skills · ${this.#items.length}`));
    for (const item of this.#items) {
      const card = el("article", "skills-item");
      const heading = el("div", "flex flex-wrap items-start justify-between gap-2");
      const title = el("div", "min-w-0");
      title.append(el("h4", "skills-item-title", item.name), el("p", "text-sm text-fg-muted", item.description));
      const controls = el("div", "flex flex-wrap gap-2");
      const toggle = el("button", "btn btn-sm", item.enabled ? "Disable" : "Enable");
      toggle.type = "button";
      toggle.setAttribute("aria-label", `${item.enabled ? "Disable" : "Enable"} ${item.name}`);
      toggle.addEventListener("click", () => send("v", "skills-enabled", String(item.id), item.enabled ? "0" : "1"));
      const edit = el("button", "btn btn-sm", "Edit");
      edit.type = "button";
      edit.addEventListener("click", () => {
        this.#editing = item.id;
        this.#name.value = item.name;
        this.#description.value = item.description;
        this.#instructions.value = item.instructions;
        this.#save.textContent = "Update skill";
        this.#formTitle.textContent = "Edit skill";
        this.#cancel.hidden = false;
        this.#name.focus();
      });
      const remove = el("button", "btn btn-sm", "Delete");
      remove.type = "button";
      remove.addEventListener("click", () => send("v", "skills-delete", String(item.id)));
      controls.append(toggle, edit, remove);
      heading.append(title, controls);
      card.append(heading, el("p", `skills-status ${item.enabled ? "is-enabled" : ""}`, item.enabled ? "Enabled" : "Disabled"));
      this.#list.append(card);
    }
  }
}

customElements.define("ph-skills-settings", PhSkillsSettings);
