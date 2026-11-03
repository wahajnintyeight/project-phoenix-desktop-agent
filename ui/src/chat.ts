// <ph-chat>: the conversation with the agent. Go owns the conversation; this element only
// draws what Go reports (`events`) and sends what the user types ("g send", "g stop",
// "g new"). Every message is placed with textContent: model output is untrusted text.
import { el } from "./dom";
import { icon } from "./icons";
import { send } from "./protocol";

export interface ChatEvent {
  k: "user" | "note" | "tool" | "start" | "delta" | "done" | "error" | "stopped" | "reset";
  text?: string;
  provider?: string;
  model?: string;
  ms?: number;
  in?: number;
  out?: number;
  cost?: number;
}

const IDEAS = ["Summarise this page", "What are the key points here?", "Draft a short reply to this"];

export class PhChat extends HTMLElement {
  readonly #log = el("div", "chat-log");
  readonly #empty = el("div", "chat-empty");
  readonly #input = el("textarea", "chat-input");
  readonly #send = el("button", "send-btn");
  readonly #share = el("button", "share-chip");
  readonly #newChat = el("button", "btn btn-quiet btn-sm", "New chat");
  #shareOn = false;
  #running = false;
  #current: { box: HTMLElement; body: Text; meta: HTMLElement } | null = null;
  #lastUser: HTMLElement | null = null; // where a "page shared" note goes
  #who = "Agent"; // the label for the agent's messages this turn: provider and model

  connectedCallback(): void {
    if (this.childElementCount > 0) return;

    this.#log.setAttribute("role", "log");
    this.#log.setAttribute("aria-label", "Conversation");
    // The log would announce every streamed word; the status pill and the finished reply do
    // that job instead.
    this.#log.setAttribute("aria-live", "off");

    // The empty state teaches the one non-obvious feature: sharing the open tab.
    const ideas = el("div", "chat-ideas");
    for (const text of IDEAS) {
      const b = el("button", "chat-idea", text);
      b.type = "button";
      b.addEventListener("click", () => {
        this.#setShare(true);
        this.#input.value = text;
        this.#syncSend();
        this.#input.focus();
      }, { passive: true });
      ideas.append(b);
    }
    this.#empty.append(
      el("h2", undefined, "Ask Phoenix"),
      el("p", undefined, "Ask about the tab you have open. Turn on Share page and the agent reads it. It cannot click, type or open files yet."),
      ideas,
    );
    this.#log.append(this.#empty);

    this.#input.rows = 1;
    this.#input.maxLength = 20000;
    this.#input.placeholder = "Message the agent";
    this.#input.setAttribute("aria-label", "Message");
    this.#input.setAttribute("aria-description", "Enter sends. Shift and Enter add a new line.");
    this.#input.spellcheck = true;

    // Sharing is the user's choice, kept between sessions: the page's text goes to the model.
    this.#share.type = "button";
    this.#share.title = "Send this tab's text with your message";
    this.#share.append(icon("page", 14), el("span", undefined, "Share page"));
    try { this.#shareOn = localStorage.getItem("phoenix.sharePage") === "1"; } catch { /* storage unavailable */ }
    this.#setShare(this.#shareOn);

    const picker = document.createElement("ph-quick-model");
    picker.id = "quick-model"; // Go addresses the picker by this id

    this.#send.type = "button";
    this.#syncSend();

    const bar = el("div", "composer-bar");
    bar.append(this.#share, picker, this.#send);
    const composer = el("div", "composer");
    composer.append(this.#input, bar);
    const wrap = el("div", "chat-composer");
    wrap.append(composer);

    this.#newChat.type = "button";
    this.#newChat.hidden = true;
    const head = el("div", "chat-head");
    head.append(this.#newChat);
    this.append(head, this.#log, wrap);

    this.#input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        this.#submit();
      }
    });
    this.#input.addEventListener("input", () => {
      this.#input.rows = Math.min(6, this.#input.value.split("\n").length); // grows without reading layout
      this.#syncSend();
    }, { passive: true });
    this.#send.addEventListener("click", () => (this.#running ? send("g", "stop") : this.#submit()), { passive: true });
    this.#share.addEventListener("click", () => this.#setShare(!this.#shareOn), { passive: true });
    this.#newChat.addEventListener("click", () => send("g", "new"), { passive: true });
  }

  set events(list: ChatEvent[]) {
    const stick = this.#nearBottom();
    for (const ev of list) this.#apply(ev);
    if (stick) this.#log.scrollTop = this.#log.scrollHeight;
  }

  #setShare(on: boolean): void {
    this.#shareOn = on;
    this.#share.setAttribute("aria-pressed", String(on));
    try { localStorage.setItem("phoenix.sharePage", on ? "1" : "0"); } catch { /* ignore */ }
  }

  /** Send is Stop while a reply is on its way, and disabled while there is nothing to send. */
  #syncSend(): void {
    this.#send.replaceChildren(icon(this.#running ? "stop" : "arrowUp", 16));
    this.#send.dataset["mode"] = this.#running ? "stop" : "send";
    this.#send.setAttribute("aria-label", this.#running ? "Stop" : "Send message");
    this.#send.disabled = !this.#running && this.#input.value.trim() === "";
  }

  #submit(): void {
    const text = this.#input.value.trim();
    if (!text || this.#running) return;
    this.#setRunning(true);
    send("g", "send", text, this.#shareOn ? "1" : "0");
    this.#input.value = "";
    this.#input.rows = 1;
    this.#syncSend();
  }

  #nearBottom(): boolean {
    const l = this.#log;
    return l.scrollHeight - l.scrollTop - l.clientHeight < 48;
  }

  #setRunning(on: boolean): void {
    this.#running = on;
    this.#log.setAttribute("aria-busy", String(on));
    this.#syncSend();
  }

  #message(who: string, cls: string): { box: HTMLElement; body: Text; meta: HTMLElement } {
    this.#empty.hidden = true;
    this.#newChat.hidden = false;
    const box = el("article", `chat-msg ${cls}`);
    const body = document.createTextNode("");
    const text = el("div", "chat-text");
    text.append(body);
    const meta = el("p", "chat-meta");
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
      case "tool": {
        // One quiet line per tool call, so the user can see what the agent did. The next
        // reply text starts a new message below it, keeping the order of events.
        const t = el("p", "chat-tool");
        t.append(icon("bolt", 12), document.createTextNode(ev.text ?? ""));
        this.#empty.hidden = true;
        // A reply box that is still empty (the agent went straight to a tool) is dropped; the
        // text that follows gets a fresh one below the tool line.
        if (this.#current && this.#current.body.data === "") this.#current.box.remove();
        this.#log.append(t);
        this.#current = null;
        break;
      }
      case "note":
        if (this.#lastUser) this.#lastUser.textContent = ev.text ?? "";
        break;
      case "start": {
        this.#who = [ev.provider, ev.model].filter(Boolean).join(" · ") || "Agent";
        this.#current = this.#message(this.#who, "chat-ai");
        this.#current.box.dataset["streaming"] = "true";
        break;
      }
      case "delta": {
        // A reply that begins without a start event (or after a reset) still needs a box.
        this.#current ??= this.#message(this.#who, "chat-ai");
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
