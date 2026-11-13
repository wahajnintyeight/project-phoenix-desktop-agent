import { el } from "./dom";
import { icon } from "./icons";
import type { Msg } from "./chat-message";

export class ChatChoiceVault {
  readonly element = el("div", "chat-choice-vault");
  readonly #close = el("button", "chat-choice-dismiss");
  #message: Msg | undefined;

  constructor() {
    this.#close.type = "button";
    this.#close.setAttribute("aria-label", "Dismiss suggestions");
    this.#close.title = "Dismiss suggestions";
    this.#close.append(icon("close", 16));
    this.#close.addEventListener("click", () => { this.#message?.dismissChoices(); this.element.replaceChildren(); });
  }

  show(message?: Msg): void {
    this.#message = message;
    this.element.replaceChildren();
    if (message) this.element.append(this.#close);
    message?.mountChoices(this.element);
  }
}
