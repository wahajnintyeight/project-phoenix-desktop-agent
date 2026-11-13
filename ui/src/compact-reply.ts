import { el } from "./dom";

export class CompactReply {
  readonly element = el("button", "chat-compact-reply");
  readonly #text = el("span", "chat-compact-reply-text");
  #buffer = "";

  constructor() {
    this.element.type = "button";
    this.element.hidden = true;
    this.element.setAttribute("aria-label", "Open chat to read the reply");
    this.element.title = "Open chat to read the reply";
    this.element.append(el("span", "chat-compact-reply-dot"), this.#text);
    this.element.addEventListener("click", () => document.querySelector<HTMLButtonElement>("#agent-collapse")?.click());
  }

  clear(): void { this.#buffer = ""; this.element.hidden = true; }

  append(text: string): void {
    this.#buffer = (this.#buffer + text).slice(-400);
    this.#text.textContent = this.#buffer.replace(/\s+/g, " ").trim();
    this.element.hidden = !this.#text.textContent;
  }

  setStreaming(on: boolean): void { this.element.dataset["streaming"] = String(on); }
}
