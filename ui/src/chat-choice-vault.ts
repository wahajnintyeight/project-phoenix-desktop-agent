import { el } from "./dom";
import type { Msg } from "./chat-message";

export class ChatChoiceVault {
  readonly element = el("div", "chat-choice-vault");

  show(message?: Msg): void {
    this.element.replaceChildren();
    message?.mountChoices(this.element);
  }
}
