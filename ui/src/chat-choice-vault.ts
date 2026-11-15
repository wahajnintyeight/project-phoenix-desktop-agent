import { el } from "./dom";
import { icon } from "./icons";
import type { Msg } from "./chat-message";

export class ChatChoiceVault {
  readonly element = el("div", "chat-choice-vault");
  readonly #close = el("button", "chat-choice-dismiss");
  readonly #toggle = el("button", "chat-choice-toggle");
  #message: Msg | undefined;

  constructor() {
    this.#toggle.type = "button";
    this.#toggle.addEventListener("click", () => this.#setCollapsed(this.element.dataset["collapsed"] !== "true"));
    this.#setCollapsed(false);
    this.#close.type = "button";
    this.#close.setAttribute("aria-label", "Dismiss suggestions");
    this.#close.title = "Dismiss suggestions";
    this.#close.append(icon("close", 16));
    this.#close.addEventListener("click", () => { this.#message?.dismissChoices(); this.element.replaceChildren(); });
  }

  show(message?: Msg): void {
    if (message !== this.#message) this.#setCollapsed(false);
    this.#message = message;
    this.element.replaceChildren();
    if (message) this.element.append(this.#toggle, this.#close);
    message?.mountChoices(this.element);
  }

  #setCollapsed(collapsed: boolean): void {
    this.element.dataset["collapsed"] = String(collapsed);
    this.#toggle.setAttribute("aria-expanded", String(!collapsed));
    this.#toggle.setAttribute("aria-label", collapsed ? "Show questions and suggestions" : "Minimize questions and suggestions");
    this.#toggle.title = collapsed ? "Show questions and suggestions" : "Minimize questions and suggestions";
    this.#toggle.replaceChildren(icon(collapsed ? "chevronUp" : "chevronDown", 16), ...(collapsed ? [el("span", undefined, "Show questions")] : []));
  }
}
