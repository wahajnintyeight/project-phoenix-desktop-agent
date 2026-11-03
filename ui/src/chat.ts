// <ph-chat>: the conversation with the agent. Go owns the conversation; this element only
// draws what Go reports (`events`) and sends what the user types ("g send", "g stop",
// "g new"). Every message is placed with textContent: model output is untrusted text.
import { send } from "./protocol";

export interface ChatEvent {
  k: "user" | "note" | "start" | "delta" | "done" | "error" | "stopped" | "reset";
  text?: string;
  provider?: string;
  model?: string;
  ms?: number;
  in?: number;
  out?: number;
  cost?: number;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

export class PhChat extends HTMLElement {
  readonly #log = el("div", "chat-log");
  readonly #empty = el("div", "chat-empty");
  readonly #input = el("textarea", "chat-input");
  readonly #send = el("button", "btn btn-primary chat-send", "Send");
  readonly #newChat = el("button", "btn btn-quiet chat-new", "New chat");
  readonly #share = el("input");
  #running = false;
  #current: { box: HTMLElement; body: Text; meta: HTMLElement } | null = null;
  #lastUser: HTMLElement | null = null; // where a "page shared" note goes

  connectedCallback(): void {
    if (this.childElementCount > 0) return;

    this.#log.setAttribute("role", "log");
    this.#log.setAttribute("aria-live", "polite");
    this.#log.setAttribute("aria-label", "Conversation");
    const hint = el("p", "muted", "Ask a question, or turn on Share this page so the agent can read the tab you are looking at. It cannot click, type or open files yet.");
    this.#empty.append(el("h2", undefined, "Ask Phoenix"), hint);
    this.#log.append(this.#empty);

    this.#input.rows = 1;
    this.#input.maxLength = 20000;
    this.#input.placeholder = "Message the agent";
    this.#input.setAttribute("aria-label", "Message");
    this.#input.spellcheck = true;
    this.#send.type = "button";
    this.#newChat.type = "button";
    this.#newChat.hidden = true;

    // Sharing is the user's choice, per message: the page's text is sent to the chosen model.
    this.#share.type = "checkbox";
    try { this.#share.checked = localStorage.getItem("phoenix.sharePage") === "1"; } catch { /* storage unavailable */ }
    const shareLabel = el("label", "chat-share");
    shareLabel.append(this.#share, el("span", undefined, "Share this page with the agent"));

    const composer = el("div", "chat-composer");
    const row = el("div", "chat-row");
    row.append(this.#input, this.#send);
    composer.append(shareLabel, row, el("p", "chat-hint small muted", "Enter to send, Shift+Enter for a new line"));
    this.#share.addEventListener("change", () => {
      try { localStorage.setItem("phoenix.sharePage", this.#share.checked ? "1" : "0"); } catch { /* ignore */ }
    }, { passive: true });

    const head = el("div", "chat-head");
    head.append(this.#newChat);
    this.append(head, this.#log, composer);

    this.#input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        this.#submit();
      }
    });
    this.#input.addEventListener("input", () => {
      this.#input.rows = Math.min(6, this.#input.value.split("\n").length); // grows without reading layout
    }, { passive: true });
    this.#send.addEventListener("click", () => (this.#running ? send("g", "stop") : this.#submit()), { passive: true });
    this.#newChat.addEventListener("click", () => send("g", "new"), { passive: true });
  }

  set events(list: ChatEvent[]) {
    const stick = this.#nearBottom();
    for (const ev of list) this.#apply(ev);
    if (stick) this.#log.scrollTop = this.#log.scrollHeight;
  }

  #submit(): void {
    const text = this.#input.value.trim();
    if (!text || this.#running) return;
    this.#setRunning(true);
    send("g", "send", text, this.#share.checked ? "1" : "0");
    this.#input.value = "";
    this.#input.rows = 1;
  }

  #nearBottom(): boolean {
    const l = this.#log;
    return l.scrollHeight - l.scrollTop - l.clientHeight < 48;
  }

  #setRunning(on: boolean): void {
    this.#running = on;
    this.#send.textContent = on ? "Stop" : "Send";
    this.#send.classList.toggle("btn-primary", !on);
    this.#log.setAttribute("aria-busy", String(on));
  }

  #message(who: string, cls: string): { box: HTMLElement; body: Text; meta: HTMLElement } {
    this.#empty.hidden = true;
    this.#newChat.hidden = false;
    const box = el("article", `chat-msg ${cls}`);
    const body = document.createTextNode("");
    const text = el("div", "chat-text");
    text.append(body);
    const meta = el("p", "chat-meta small muted");
    box.append(el("h3", "chat-who", who), text, meta);
    this.#log.append(box);
    return { box, body, meta };
  }

  #apply(ev: ChatEvent): void {
    switch (ev.k) {
      case "reset":
        this.#log.replaceChildren(this.#empty);
        this.#empty.hidden = false;
        this.#newChat.hidden = true;
        this.#current = null;
        this.#setRunning(false);
        break;
      case "user": {
        const m = this.#message("You", "chat-user");
        m.body.data = ev.text ?? "";
        this.#lastUser = m.meta;
        this.#current = null;
        this.#setRunning(true);
        break;
      }
      case "note":
        if (this.#lastUser) this.#lastUser.textContent = ev.text ?? "";
        break;
      case "start": {
        this.#current = this.#message([ev.provider, ev.model].filter(Boolean).join(" · ") || "Agent", "chat-ai");
        this.#current.box.dataset["streaming"] = "true";
        break;
      }
      case "delta": {
        // A reply that begins without a start event (or after a reset) still needs a box.
        this.#current ??= this.#message("Agent", "chat-ai");
        this.#current.box.dataset["streaming"] = "true";
        this.#current.body.appendData(ev.text ?? "");
        break;
      }
      case "done": {
        const c = this.#current;
        if (c) {
          delete c.box.dataset["streaming"];
          const bits = [ev.ms ? `${(ev.ms / 1000).toFixed(1)} s` : "", ev.out ? `${ev.in ?? 0} in, ${ev.out} out` : "", ev.cost ? `about $${ev.cost.toFixed(4)}` : ""];
          c.meta.textContent = bits.filter(Boolean).join(" · ");
        }
        this.#current = null;
        this.#setRunning(false);
        break;
      }
      case "stopped": {
        if (this.#current) {
          delete this.#current.box.dataset["streaming"];
          this.#current.meta.textContent = "Stopped";
        }
        this.#current = null;
        this.#setRunning(false);
        break;
      }
      case "error": {
        if (this.#current) delete this.#current.box.dataset["streaming"];
        const m = this.#message("Something went wrong", "chat-error");
        m.body.data = ev.text ?? "";
        this.#current = null;
        this.#setRunning(false);
        break;
      }
    }
  }
}

customElements.define("ph-chat", PhChat);
