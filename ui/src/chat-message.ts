import { ChoiceMenus, splitChoices, type ChoiceAnswer, type ChoiceMode, type ChoiceSet } from "./choices";
import { el } from "./dom";
import { icon } from "./icons";
import { MarkdownView } from "./markdown";

/** One message in the log. The agent's replies are Markdown; everything else is plain text. */
export class Msg {
  readonly box: HTMLElement;
  readonly text = el("div", "chat-text");
  readonly meta = el("p", "chat-meta");
  readonly time = document.createElement("time");
  readonly copy = el("button", "chat-copy");
  readonly who: string;
  #raw = "";
  #md: MarkdownView | null = null;
  #frame = 0;
  #copyTimer = 0;
  #menus: ChoiceMenus | null = null;
  #suggestions: ChoiceMenus | null = null;
  #choiceMode: ChoiceMode = "inactive";
  #choiceHost: HTMLElement | null = null;
  #choicesDismissed = false;

  constructor(who: string, cls: string, markdown: boolean, at: number) {
    this.who = who;
    this.box = el("article", `chat-msg ${cls}`);
    const footer = el("div", "chat-msg-footer");
    const copyable = cls.includes("chat-user") || cls.includes("chat-ai");
    this.copy.type = "button";
    this.copy.hidden = !copyable || this.#raw === "";
    this.copy.setAttribute("aria-label", "Copy message");
    // Icon only; the label is for screen readers and tells them when the copy worked.
    this.copy.append(icon("copy", 14), icon("check", 14), el("span", "sr-only", "Copy"));
    this.copy.addEventListener("click", () => this.#copyText(), { passive: true });
    this.time.className = "chat-time";
    this.setTime(at);
    footer.append(this.meta, this.time, this.copy);
    // The name is for screen readers; sighted users read it in the footer line.
    this.box.append(el("h3", cls.includes("chat-error") ? "chat-who" : "sr-only", who), this.text, footer);
    if (markdown) this.#md = new MarkdownView(this.text);
  }

  setTime(at: number): void {
    const date = new Date(at);
    if (!at || Number.isNaN(date.valueOf())) {
      this.time.removeAttribute("datetime");
      this.time.textContent = "Date unavailable";
      return;
    }
    this.time.dateTime = date.toISOString();
    this.time.textContent = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" }).format(date);
  }

  get empty(): boolean {
    return this.#raw === "";
  }

  set(text: string): void {
    this.#raw = text;
    this.text.textContent = text;
    if (this.copy.isConnected && (this.who === "You" || this.box.classList.contains("chat-ai"))) this.copy.hidden = text === "";
  }

  /** Adds streamed text; the Markdown is redrawn at most once per frame. */
  append(text: string): void {
    this.#raw += text;
    if (this.#raw && (this.who === "You" || this.box.classList.contains("chat-ai"))) this.copy.hidden = false;
    if (!this.#md) {
      this.text.append(document.createTextNode(text));
    } else if (!this.#frame) {
      this.#frame = requestAnimationFrame(() => this.flush());
    }
  }

  flush(): void {
    if (this.#frame) cancelAnimationFrame(this.#frame);
    this.#frame = 0;
    if (!this.#md) return;
    const { body, choices } = splitChoices(this.#raw);
    this.#md.set(body);
    if (choices.length && !this.#choicesDismissed) {
      this.#suggestions?.element.remove();
      this.#suggestions = null;
      if (!this.#menus) {
        this.#menus = new ChoiceMenus((answers) => this.box.dispatchEvent(new CustomEvent<ChoiceAnswer[]>("phoenix-answers", { bubbles: true, detail: answers })));
      }
      this.#menus.update(choices);
      this.#menus.setMode(this.#choiceMode);
      this.#placeChoices(this.#menus);
    } else {
      this.#menus?.element.remove();
      this.#menus = null;
    }
  }

  getChoices(): ChoiceSet[] {
    return this.#md ? splitChoices(this.#raw).choices : [];
  }

  setChoiceMode(mode: ChoiceMode): void { this.#choiceMode = mode; this.#menus?.setMode(mode); }

  mountChoices(host: HTMLElement | null): void {
    this.#choiceHost = host;
    if (this.#menus) this.#placeChoices(this.#menus);
    if (this.#suggestions) this.#placeChoices(this.#suggestions);
  }

  setSuggestions(options: string[], mode: ChoiceMode = "ready"): void {
    if (this.#choicesDismissed || this.getChoices().length) return;
    if (!this.#suggestions) {
      this.#suggestions = new ChoiceMenus((answers) => this.box.dispatchEvent(new CustomEvent<ChoiceAnswer[]>("phoenix-answers", { bubbles: true, detail: answers })));
    }
    this.#suggestions.update([{ question: "What would you like to do next?", options, kind: "action" }]);
    this.#suggestions.setMode(mode);
    this.#placeChoices(this.#suggestions);
  }

  setSuggestionMode(mode: ChoiceMode): void { this.#suggestions?.setMode(mode); }

  dismissChoices(): void {
    this.#choicesDismissed = true;
    this.#menus?.element.remove();
    this.#suggestions?.element.remove();
  }

  #placeChoices(menus: ChoiceMenus): void {
    const host = this.#choiceHost;
    if (host) host.append(menus.element);
    else if (menus.element.parentElement !== this.box) {
      if (menus === this.#menus) this.text.after(menus.element);
      else this.box.append(menus.element);
    }
  }

  #copyText(): void {
    const text = this.#md ? splitChoices(this.#raw).body : this.#raw;
    if (!text || !navigator.clipboard?.writeText) return;
    const show = (label: string, copied: boolean): void => {
      this.copy.lastElementChild!.textContent = label;
      if (copied) this.copy.dataset["copied"] = "";
      else delete this.copy.dataset["copied"];
      window.clearTimeout(this.#copyTimer);
      this.#copyTimer = window.setTimeout(() => show("Copy", false), 1500);
    };
    void navigator.clipboard.writeText(text).then(() => show("Copied", true), () => show("Copy failed", false));
  }
}

