// <ph-chat>: the conversation with the agent. Go owns the conversation; this element only
// draws what Go reports (`events`) and sends what the user types ("g send", "g stop",
// "g new"). Every message is placed with textContent: model output is untrusted text.
import { el } from "./dom";
import { icon } from "./icons";
import { MarkdownView } from "./markdown";
import type { PhMcp } from "./mcp-settings";
import { send } from "./protocol";
import { resolveSlashCommand, searchSlashCommands, type SlashCommandDefinition } from "./slash-commands";

export interface ChatEvent {
  k: "user" | "note" | "processing" | "tool" | "tool-result" | "approve" | "approved" | "start" | "delta" | "done" | "error" | "stopped" | "reset" | "restore";
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
  toolId?: string;
  server?: string;
  toolName?: string;
  input?: string;
  output?: string;
  toolError?: boolean;
  messages?: ChatTranscriptMessage[];
}

interface ChatTranscriptMessage {
  role: "user" | "assistant";
  content: string;
  provider?: string;
  model?: string;
}

interface SavedChatSummary {
  id: string;
  title: string;
  updatedAt: string;
}

/** One message in the log. The agent's replies are Markdown; everything else is plain text. */
class Msg {
  readonly box: HTMLElement;
  readonly text = el("div", "chat-text");
  readonly meta = el("p", "chat-meta");
  readonly copy = el("button", "chat-copy");
  readonly who: string;
  #raw = "";
  #md: MarkdownView | null = null;
  #frame = 0;
  #copyTimer = 0;

  constructor(who: string, cls: string, markdown: boolean) {
    this.who = who;
    this.box = el("article", `chat-msg ${cls}`);
    const footer = el("div", "chat-msg-footer");
    const copyable = cls.includes("chat-user") || cls.includes("chat-ai");
    this.copy.type = "button";
    this.copy.hidden = !copyable || this.#raw === "";
    this.copy.setAttribute("aria-label", "Copy message");
    this.copy.append(icon("copy", 12), el("span", undefined, "Copy"));
    this.copy.addEventListener("click", () => this.#copyText(), { passive: true });
    footer.append(this.meta, this.copy);
    // The name is for screen readers; sighted users read it in the footer line.
    this.box.append(el("h3", cls.includes("chat-error") ? "chat-who" : "sr-only", who), this.text, footer);
    if (markdown) this.#md = new MarkdownView(this.text);
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
    this.#md?.set(this.#raw);
  }

  #copyText(): void {
    if (!this.#raw || !navigator.clipboard?.writeText) return;
    void navigator.clipboard.writeText(this.#raw).then(() => {
      this.copy.lastElementChild!.textContent = "Copied";
      window.clearTimeout(this.#copyTimer);
      this.#copyTimer = window.setTimeout(() => { this.copy.lastElementChild!.textContent = "Copy"; }, 1500);
    }, () => {
      this.copy.lastElementChild!.textContent = "Copy failed";
      window.clearTimeout(this.#copyTimer);
      this.#copyTimer = window.setTimeout(() => { this.copy.lastElementChild!.textContent = "Copy"; }, 1500);
    });
  }
}

interface ToolCardState {
  card: HTMLDetailsElement;
  status: HTMLElement;
  output: HTMLPreElement;
  outputSection: HTMLElement;
}

function prettyToolText(value: string): string {
  try { return JSON.stringify(JSON.parse(value), null, 2) ?? value; } catch { return value; }
}

const IDEAS = ["Summarise this page", "What are the key points here?", "Draft a short reply to this"];

export class PhChat extends HTMLElement {
  readonly #log = el("div", "chat-log");
  readonly #empty = el("div", "chat-empty");
  readonly #processing = el("div", "chat-processing");
  readonly #processingText = el("span");
  readonly #input = el("textarea", "chat-input");
  readonly #commandMenu = el("div", "slash-menu");
  readonly #send = el("button", "send-btn");
  readonly #share = el("button", "share-chip");
  readonly #newChat = el("button", "btn btn-quiet btn-sm", "New chat");
  readonly #chatHistory = el("details", "chat-history");
  readonly #chatHistoryCount = el("span", "chat-history-count", "0");
  readonly #chatHistoryList = el("div", "chat-history-list");
  #shareOn = false;
  #running = false;
  #commands: SlashCommandDefinition[] = [];
  readonly #toolCalls = new Map<string, ToolCardState>();
  #activeCommand = 0;
  #current: Msg | null = null;
  #scrollFrame = 0;
  #inputWidth = 0;
  readonly #inputObserver = new ResizeObserver((entries) => {
    const width = entries[0]?.contentRect.width ?? 0;
    if (width > 0 && width !== this.#inputWidth) {
      this.#inputWidth = width;
      this.#resizeInput();
    }
  });
  #lastUser: HTMLElement | null = null; // where a "page shared" note goes
  #who = "Agent"; // the label for the agent's messages this turn: provider and model

  connectedCallback(): void {
    if (this.childElementCount > 0) {
      this.#inputObserver.observe(this.#input);
      return;
    }

    this.#log.setAttribute("role", "log");
    this.#log.setAttribute("aria-label", "Conversation");
    // The log would announce every streamed word; the status pill and the finished reply do
    // that job instead.
    this.#log.setAttribute("aria-live", "off");
    this.#processing.setAttribute("role", "status");
    this.#processing.setAttribute("aria-live", "polite");
    this.#processing.hidden = true;
    this.#processing.append(el("span", "chat-spinner"), this.#processingText);

    // The empty state teaches the one non-obvious feature: sharing the open tab.
    const ideas = el("div", "chat-ideas");
    for (const text of IDEAS) {
      const b = el("button", "chat-idea", text);
      b.type = "button";
      b.addEventListener("click", () => {
        this.#setShare(true);
        this.#input.value = text;
        this.#resizeInput();
        this.#syncSend();
        this.#input.focus();
      }, { passive: true });
      ideas.append(b);
    }
    this.#empty.append(
      el("h2", undefined, "What can we work on?"),
      el("p", undefined, "Give Phoenix a task. It can inspect and use your browser to make progress."),
      ideas,
    );
    this.#log.append(this.#empty);

    this.#input.rows = 1;
    this.#input.maxLength = 20000;
    this.#input.placeholder = "Message Phoenix · / for commands";
    this.#input.setAttribute("aria-label", "Message");
    this.#input.setAttribute("aria-description", "Enter sends. Shift and Enter add a new line. Type / to browse commands.");
    this.#commandMenu.id = "slash-commands";
    this.#commandMenu.setAttribute("role", "listbox");
    this.#commandMenu.setAttribute("aria-label", "Slash commands");
    this.#commandMenu.hidden = true;
    this.#input.setAttribute("aria-autocomplete", "list");
    this.#input.setAttribute("aria-controls", this.#commandMenu.id);
    this.#input.setAttribute("aria-haspopup", "listbox");
    this.#input.setAttribute("aria-expanded", "false");
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
    wrap.append(this.#commandMenu, composer);

    this.#newChat.type = "button";
    this.#newChat.hidden = true;
    this.#chatHistory.append(
      el("summary", "chat-history-trigger", "Chats"),
      this.#chatHistoryList,
    );
    this.#chatHistory.querySelector("summary")?.append(this.#chatHistoryCount);
    const head = el("div", "chat-head");
    head.append(this.#chatHistory, this.#newChat);
    this.append(head, this.#log, wrap);
    this.#inputObserver.observe(this.#input);

    this.#input.addEventListener("keydown", (e) => {
      if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
        e.preventDefault();
        this.#submitInput();
      } else if (e.key === "ArrowDown" && !this.#commandMenu.hidden && this.#commands.length) {
        e.preventDefault();
        this.#setActiveCommand(this.#activeCommand + 1);
      } else if (e.key === "ArrowUp" && !this.#commandMenu.hidden && this.#commands.length) {
        e.preventDefault();
        this.#setActiveCommand(this.#activeCommand - 1);
      } else if (e.key === "Escape" && !this.#commandMenu.hidden) {
        e.preventDefault();
        this.#hideCommandMenu();
      }
    });
    this.#input.addEventListener("input", () => {
      this.#resizeInput();
      this.#renderCommandMenu();
      this.#syncSend();
    }, { passive: true });
    this.#commandMenu.addEventListener("click", (e) => {
      const option = (e.target as Element | null)?.closest<HTMLButtonElement>("button[data-command-index]");
      const command = this.#commands[Number(option?.dataset["commandIndex"])];
      if (command) this.#runCommand(command);
    }, { passive: true });
    this.#send.addEventListener("click", () => (this.#running ? send("g", "stop") : this.#submitInput()), { passive: true });
    this.#share.addEventListener("click", () => this.#setShare(!this.#shareOn), { passive: true });
    this.#newChat.addEventListener("click", () => send("g", "new"), { passive: true });
  }

  disconnectedCallback(): void {
    this.#inputObserver.disconnect();
    this.#inputWidth = 0;
  }

  set events(list: ChatEvent[]) {
    const stick = this.#nearBottom() || list.some((ev) => ev.k === "user" || ev.k === "done" || ev.k === "stopped" || ev.k === "error" || ev.k === "restore");
    for (const ev of list) this.#apply(ev);
    if (stick && !this.#scrollFrame) {
      this.#scrollFrame = requestAnimationFrame(() => {
        this.#scrollFrame = 0;
        this.#current?.flush();
        this.#log.scrollTop = this.#log.scrollHeight;
      });
    }
  }

  set chats(list: SavedChatSummary[]) {
    this.#chatHistoryCount.textContent = String(list.length);
    this.#chatHistoryList.replaceChildren();
    if (!list.length) {
      this.#chatHistoryList.append(el("p", "chat-history-empty", "Saved conversations will appear here."));
      return;
    }
    for (const chat of list) {
      const button = el("button", "chat-history-item");
      button.type = "button";
      button.append(el("span", "chat-history-title", chat.title));
      const date = new Date(chat.updatedAt);
      if (!Number.isNaN(date.valueOf())) {
        button.append(el("span", "chat-history-date", new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(date)));
      }
      button.disabled = this.#running;
      button.addEventListener("click", () => {
        if (this.#running) return;
        send("g", "restore", chat.id);
        this.#chatHistory.open = false;
      }, { passive: true });
      this.#chatHistoryList.append(button);
    }
  }

  #setShare(on: boolean): void {
    this.#shareOn = on;
    this.#share.setAttribute("aria-pressed", String(on));
    try { localStorage.setItem("phoenix.sharePage", on ? "1" : "0"); } catch { /* ignore */ }
  }

  #resizeInput(): void {
    this.#input.style.height = "auto";
    this.#input.style.height = `${this.#input.scrollHeight}px`;
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
    this.#resizeInput();
    this.#syncSend();
  }

  #submitInput(): void {
    if (!this.#input.value.startsWith("/")) {
      this.#submit();
      return;
    }
    const command = resolveSlashCommand(this.#input.value) ?? (!this.#commandMenu.hidden ? this.#commands[this.#activeCommand] : undefined);
    if (command) this.#runCommand(command);
    else this.#renderCommandMenu();
  }

  #renderCommandMenu(): void {
    if (!this.#input.value.startsWith("/")) {
      this.#hideCommandMenu();
      return;
    }
    const matches = searchSlashCommands(this.#input.value);
    this.#commands = this.#running ? matches.filter((command) => command.action.type !== "quick-action") : matches;
    this.#activeCommand = 0;
    this.#commandMenu.replaceChildren(...this.#commands.map((command, index) => {
      const option = el("button", "slash-option");
      option.type = "button";
      option.tabIndex = -1;
      option.id = `slash-option-${command.id}`;
      option.dataset["commandIndex"] = String(index);
      option.setAttribute("role", "option");
      option.setAttribute("aria-selected", String(index === 0));
      option.append(el("span", "slash-name", `/${command.id}`), el("span", "slash-description", command.description));
      return option;
    }));
    if (!this.#commands.length) {
      const text = matches.length && this.#running
        ? "Prompt commands are available when the current reply finishes."
        : "No matching command. Type / to browse commands.";
      const empty = el("p", "slash-empty", text);
      empty.setAttribute("role", "status");
      this.#commandMenu.append(empty);
    }
    this.#commandMenu.hidden = false;
    this.#input.setAttribute("aria-expanded", "true");
    if (this.#commands.length) this.#input.setAttribute("aria-activedescendant", `slash-option-${this.#commands[0]?.id}`);
    else this.#input.removeAttribute("aria-activedescendant");
    this.#commandMenu.scrollTop = 0;
  }

  #setActiveCommand(index: number): void {
    const length = this.#commands.length;
    this.#activeCommand = (index + length) % length;
    this.#commandMenu.querySelectorAll<HTMLButtonElement>("[data-command-index]").forEach((option, i) => {
      const active = i === this.#activeCommand;
      option.setAttribute("aria-selected", String(active));
      if (active) {
        this.#input.setAttribute("aria-activedescendant", option.id);
        option.scrollIntoView({ block: "nearest" });
      }
    });
  }

  #hideCommandMenu(): void {
    this.#commands = [];
    this.#commandMenu.hidden = true;
    this.#input.setAttribute("aria-expanded", "false");
    this.#input.removeAttribute("aria-activedescendant");
  }

  #runCommand(command: SlashCommandDefinition): void {
    if (this.#running && command.action.type === "quick-action") return;
    this.#hideCommandMenu();
    if (command.action.type === "help") {
      this.#input.value = "/";
      this.#resizeInput();
      this.#renderCommandMenu();
      this.#input.focus();
      return;
    }

    this.#input.value = "";
    this.#resizeInput();
    this.#syncSend();
    switch (command.action.type) {
      case "clear": send("g", "new"); break;
      case "stop": send("g", "stop"); break;
      case "share": this.#setShare(!this.#shareOn); break;
      case "settings": send("c", "menu.settings"); break;
      case "model": document.querySelector<HTMLButtonElement>("#quick-model button")?.click(); break;
      case "list-mcps": this.#listWorkingMCPs(); break;
      case "quick-action": send("g", "act", command.action.id); break;
    }
    if (command.action.type !== "settings" && command.action.type !== "model") this.#input.focus();
  }

  #listWorkingMCPs(): void {
    const servers = document.querySelector<PhMcp>("#mcp-servers")?.workingServers ?? [];
    const text = servers.length
      ? `Enabled MCP servers that passed their latest test this session:\n${servers.map((server) => `• ${server.name} — ${server.toolCount} ${server.toolCount === 1 ? "tool" : "tools"}`).join("\n")}`
      : "No enabled MCP servers have passed a connection test this session. Open Settings → Tools (MCP) and test a server first.";
    const message = this.#message("Phoenix", "chat-local");
    message.set(text);
    this.#log.scrollTop = this.#log.scrollHeight;
  }

  #nearBottom(): boolean {
    const l = this.#log;
    return l.scrollHeight - l.scrollTop - l.clientHeight < 48;
  }

  #setRunning(on: boolean): void {
    this.#running = on;
    this.#log.setAttribute("aria-busy", String(on));
    this.#chatHistoryList.querySelectorAll<HTMLButtonElement>(".chat-history-item").forEach((button) => { button.disabled = on; });
    this.#syncSend();
    if (!this.#commandMenu.hidden) this.#renderCommandMenu();
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
    else this.#current?.flush();
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
        this.#processing.hidden = true;
        this.#toolCalls.clear();
        this.#empty.hidden = false;
        this.#newChat.hidden = true;
        this.#current = null;
        this.#lastUser = null;
        this.#setRunning(false);
        break;
      case "restore": {
        this.#log.replaceChildren(this.#empty);
        this.#processing.hidden = true;
        this.#toolCalls.clear();
        this.#empty.hidden = false;
        this.#newChat.hidden = true;
        this.#current = null;
        this.#lastUser = null;
        this.#who = "Agent";
        for (const message of ev.messages ?? []) {
          if (message.role === "user") {
            const m = this.#message("You", "chat-user");
            m.set(message.content);
            this.#lastUser = m.meta;
          } else if (message.role === "assistant") {
            const who = [message.provider, message.model].filter(Boolean).join(" · ") || "Agent";
            const m = this.#message(who, "chat-ai", true);
            m.append(message.content);
            m.flush();
            m.meta.textContent = who;
          }
        }
        this.#setRunning(false);
        this.#log.scrollTop = this.#log.scrollHeight;
        break;
      }
      case "user": {
        this.#processing.hidden = true;
        this.#toolCalls.clear();
        const m = this.#message("You", "chat-user");
        m.set(ev.text ?? "");
        this.#lastUser = m.meta;
        this.#current = null;
        this.#setRunning(true);
        break;
      }
      case "processing": {
        if (!ev.text) {
          this.#processing.hidden = true;
          break;
        }
        this.#processingText.textContent = ev.text;
        this.#processing.hidden = false;
        this.#log.append(this.#processing);
        break;
      }
      case "tool": {
        this.#processing.hidden = true;
        this.#empty.hidden = true;
        // A reply box that is still empty (the agent went straight to a tool) is dropped; the
        // text that follows gets a fresh one below the tool line.
        if (this.#current?.empty) this.#current.box.remove();
        else this.#current?.flush();
        this.#current = null;
        if (!ev.toolId) {
          const t = el("p", "chat-tool");
          t.append(icon("bolt", 12), document.createTextNode(ev.text ?? ""));
          this.#log.append(t);
          break;
        }
        const card = el("details", "chat-tool-card") as HTMLDetailsElement;
        const summary = document.createElement("summary");
        const status = el("span", "chat-tool-status", "Running");
        const chevron = icon("chevronRight", 14);
        chevron.classList.add("chat-tool-chevron");
        summary.append(icon("bolt", 13), el("span", "chat-tool-title", ev.text ?? ev.toolName ?? "Tool call"), status, chevron);
        card.append(summary);
        const body = el("div", "chat-tool-body");
        if (ev.server || ev.toolName) {
          const target = el("p", "chat-tool-target");
          if (ev.server) target.append(el("span", "chat-tool-label", "MCP"), el("code", undefined, ev.server));
          if (ev.toolName) target.append(el("span", "chat-tool-label", "Tool"), el("code", undefined, ev.toolName));
          body.append(target);
        }
        if (ev.input) {
          const section = el("section", "chat-tool-section");
          section.append(el("h4", "chat-tool-section-title", "Input"), el("pre", "chat-tool-code", prettyToolText(ev.input)));
          body.append(section);
        }
        const outputSection = el("section", "chat-tool-section");
        outputSection.hidden = true;
        const output = el("pre", "chat-tool-code") as HTMLPreElement;
        outputSection.append(el("h4", "chat-tool-section-title", "Result"), output);
        body.append(outputSection);
        card.append(body);
        this.#log.append(card);
        this.#toolCalls.set(ev.toolId, { card, status, output, outputSection });
        break;
      }
      case "tool-result": {
        const call = ev.toolId ? this.#toolCalls.get(ev.toolId) : undefined;
        if (call) {
          call.status.textContent = ev.toolError ? "Error" : "Done";
          call.card.dataset["state"] = ev.toolError ? "error" : "done";
          call.output.textContent = prettyToolText(ev.output || "No text result returned.");
          call.outputSection.hidden = false;
          this.#toolCalls.delete(ev.toolId!);
        }
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
        this.#current = null;
        break;
      }
      case "delta": {
        this.#processing.hidden = true;
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
        this.#processing.hidden = true;
        this.#finishToolCalls("No result");
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
        this.#processing.hidden = true;
        this.#finishToolCalls("Stopped");
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
        this.#processing.hidden = true;
        this.#finishToolCalls("Interrupted");
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

  #finishToolCalls(label: string): void {
    for (const [id, call] of this.#toolCalls) {
      call.status.textContent = label;
      call.card.dataset["state"] = "incomplete";
      call.output.textContent = label;
      call.outputSection.hidden = false;
      this.#toolCalls.delete(id);
    }
  }
}

customElements.define("ph-chat", PhChat);
