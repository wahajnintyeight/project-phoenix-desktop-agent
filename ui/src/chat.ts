// <ph-chat>: the conversation with the agent. Go owns the conversation; this element only
// draws what Go reports (`events`) and sends what the user types ("g send", "g stop",
// "g new"). Every message is placed with textContent: model output is untrusted text.
import { el } from "./dom";
import { icon } from "./icons";
import { MarkdownView } from "./markdown";
import { send } from "./protocol";

export interface ChatEvent {
  k: "user" | "note" | "tool" | "approve" | "approved" | "start" | "delta" | "done" | "error" | "stopped" | "reset";
  text?: string;
  provider?: string;
  model?: string;
  ms?: number;
  in?: number;
  out?: number;
  cost?: number;
  id?: string;
  cls?: "look" | "act";
  decision?: "allow" | "all" | "deny";
}

/** One message in the log. The agent's replies are Markdown; everything else is plain text. */
class Msg {
  readonly box: HTMLElement;
  readonly text = el("div", "chat-text");
  readonly meta = el("p", "chat-meta");
  readonly who: string;
  #raw = "";
  #md: MarkdownView | null = null;
  #frame = 0;

  constructor(who: string, cls: string, markdown: boolean) {
    this.who = who;
    this.box = el("article", `chat-msg ${cls}`);
    // The name is for screen readers; sighted users read it in the footer line.
    this.box.append(el("h3", cls.includes("chat-error") ? "chat-who" : "sr-only", who), this.text, this.meta);
    if (markdown) this.#md = new MarkdownView(this.text);
  }

  get empty(): boolean {
    return this.#raw === "";
  }

  set(text: string): void {
    this.#raw = text;
    this.text.textContent = text;
  }

  /** Adds streamed text; the Markdown is redrawn at most once per frame. */
  append(text: string): void {
    this.#raw += text;
    if (!this.#md) {
      this.text.append(document.createTextNode(text));
    } else if (!this.#frame) {
      this.#frame = requestAnimationFrame(() => this.flush());
    }
  }

  flush(): void {
    if (this.#frame) cancelAnimationFrame(this.#frame);
    this.#frame = 0;
    this.#md?.set(this.#raw);
  }
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
  #current: Msg | null = null;
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
      el("p", undefined, "Ask about the tab you have open. Turn on Share page and the agent reads it. With Computer use on in Settings it can also look at and operate your apps, and it asks first."),
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

  #message(who: string, cls: string, markdown = false): Msg {
    this.#empty.hidden = true;
    this.#newChat.hidden = false;
    const m = new Msg(who, cls, markdown);
    this.#log.append(m.box);
    return m;
  }

  /** The agent asks before it looks at or acts on the desktop. The wording comes from Phoenix. */
  #approval(ev: ChatEvent): void {
    this.#empty.hidden = true;
    if (this.#current?.empty) this.#current.box.remove();
    this.#current = null; // the reply continues in a new message below the card
    const card = el("div", "chat-approval");
    card.dataset["id"] = ev.id ?? "";
    card.setAttribute("role", "group");
    card.setAttribute("aria-label", "The agent is asking for permission");
    const act = ev.cls === "act";
    card.append(el("p", "chat-approval-label", act ? "Needs your OK" : "Wants to look"), el("p", "chat-approval-what", ev.text ?? ""));
    const actions = el("div", "chat-approval-actions");
    const button = (label: string, decision: string, cls: string): HTMLButtonElement => {
      const b = el("button", cls, label);
      b.type = "button";
      b.addEventListener("click", () => {
        actions.querySelectorAll("button").forEach((x) => { x.disabled = true; });
        send("g", "approve", ev.id ?? "", decision);
      }, { passive: true });
      return b;
    };
    actions.append(button("Allow", "allow", "btn btn-primary btn-sm"));
    if (act) actions.append(button("Allow all this message", "all", "btn btn-sm"));
    actions.append(button("Decline", "deny", "btn btn-quiet btn-sm"));
    card.append(actions);
    this.#log.append(card);
    // The question is the most important thing on screen: bring it into view and to the
    // keyboard without stealing focus from what the user is typing.
    card.scrollIntoView({ block: "nearest" });
  }

  #answer(card: HTMLElement, text: string): void {
    card.dataset["answered"] = "";
    card.querySelector(".chat-approval-actions")?.replaceWith(el("p", "chat-approval-result", text));
  }

  /** A turn that ends leaves any unanswered card behind; Go has already refused it. */
  #expireCards(): void {
    this.#log.querySelectorAll<HTMLElement>(".chat-approval:not([data-answered])").forEach((card) => this.#answer(card, "No answer, so it was declined"));
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
        m.set(ev.text ?? "");
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
        if (this.#current?.empty) this.#current.box.remove();
        this.#log.append(t);
        this.#current = null;
        break;
      }
      case "approve":
        this.#approval(ev);
        break;
      case "approved": {
        const card = this.#log.querySelector<HTMLElement>(`.chat-approval[data-id="${CSS.escape(ev.id ?? "")}"]`);
        if (card) this.#answer(card, ev.decision === "deny" ? "Declined" : ev.decision === "all" ? "Allowed, and all actions this message" : "Allowed");
        break;
      }
      case "note":
        if (this.#lastUser) this.#lastUser.textContent = ev.text ?? "";
        break;
      case "start": {
        this.#who = [ev.provider, ev.model].filter(Boolean).join(" · ") || "Agent";
        this.#current = this.#message(this.#who, "chat-ai", true);
        this.#current.meta.textContent = this.#who;
        this.#current.box.dataset["streaming"] = "true";
        break;
      }
      case "delta": {
        // A reply that begins without a start event (or after a reset) still needs a box.
        if (!this.#current) {
          this.#current = this.#message(this.#who, "chat-ai", true);
          this.#current.meta.textContent = this.#who;
        }
        this.#current.box.dataset["streaming"] = "true";
        this.#current.append(ev.text ?? "");
        break;
      }
      case "done": {
        this.#expireCards();
        const c = this.#current;
        if (c) {
          c.flush();
          delete c.box.dataset["streaming"];
          const bits = [c.who, ev.ms ? `${(ev.ms / 1000).toFixed(1)} s` : "", ev.out ? `${ev.in ?? 0} in, ${ev.out} out` : "", ev.cost ? `about $${ev.cost.toFixed(4)}` : ""];
          c.meta.textContent = bits.filter(Boolean).join(" · ");
        }
        this.#current = null;
        this.#setRunning(false);
        break;
      }
      case "stopped": {
        this.#expireCards();
        if (this.#current) {
          this.#current.flush();
          delete this.#current.box.dataset["streaming"];
          this.#current.meta.textContent = `${this.#current.who} · Stopped`;
        }
        this.#current = null;
        this.#setRunning(false);
        break;
      }
      case "error": {
        this.#expireCards();
        if (this.#current) {
          this.#current.flush();
          delete this.#current.box.dataset["streaming"];
        }
        const m = this.#message("Something went wrong", "chat-error");
        m.set(ev.text ?? "");
        this.#current = null;
        this.#setRunning(false);
        break;
      }
    }
  }
}

customElements.define("ph-chat", PhChat);
