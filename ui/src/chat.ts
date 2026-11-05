// <ph-chat>: the conversation with the agent. Go owns the conversation; this element only
// draws what Go reports (`events`) and sends what the user types ("g send", "g stop",
// "g new"). Every message is placed with textContent: model output is untrusted text.
import { el } from "./dom";
import { icon, type IconName } from "./icons";
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
  messages?: number;
}

interface SavedChatList {
  current: string;
  items: SavedChatSummary[];
}

function when(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.valueOf())) return "";
  const days = Math.round((new Date().setHours(0, 0, 0, 0) - new Date(date).setHours(0, 0, 0, 0)) / 86_400_000);
  if (days <= 0) return "Today";
  if (days === 1) return "Yesterday";
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium" }).format(date);
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
    // Icon only; the label is for screen readers and tells them when the copy worked.
    this.copy.append(icon("copy", 14), icon("check", 14), el("span", "sr-only", "Copy"));
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
    const show = (label: string, copied: boolean): void => {
      this.copy.lastElementChild!.textContent = label;
      if (copied) this.copy.dataset["copied"] = "";
      else delete this.copy.dataset["copied"];
      window.clearTimeout(this.#copyTimer);
      this.#copyTimer = window.setTimeout(() => show("Copy", false), 1500);
    };
    void navigator.clipboard.writeText(this.#raw).then(() => show("Copied", true), () => show("Copy failed", false));
  }
}

interface ToolCardState {
  group: StepGroup;
  card: HTMLDetailsElement;
  status: HTMLElement;
  output: HTMLPreElement;
  outputSection: HTMLElement;
}

/**
 * Consecutive tool calls fold into one collapsed row, so a long run reads as a single line of
 * progress ("Clicking Apply · 7 steps") instead of a wall of cards. Opening it shows each step.
 */
class StepGroup {
  readonly box = el("details", "chat-steps") as HTMLDetailsElement;
  readonly #label = el("span", "chat-steps-label");
  readonly #count = el("span", "chat-steps-count");
  readonly body = el("div", "chat-steps-body");
  #total = 0;
  #failed = 0;
  #latest = "";
  /** Live until the run ends: between two tool calls the agent is still working, so the row
   *  keeps naming the latest step instead of flipping to "Used N tools" and back. */
  #live = true;

  constructor() {
    const summary = document.createElement("summary");
    const chevron = icon("chevronRight", 14);
    chevron.classList.add("chat-steps-chevron");
    summary.append(el("span", "chat-spinner"), this.#label, this.#count, chevron);
    this.box.append(summary, this.body);
    this.#render();
  }

  start(title: string): void {
    this.#total++;
    this.#latest = title;
    this.#render();
  }

  finish(failed: boolean): void {
    if (failed) this.#failed++;
    this.#render();
  }

  /** The run ended: nothing is running any more, whatever Go last reported. */
  settle(): void {
    this.#live = false;
    this.#render();
  }

  #render(): void {
    const running = this.#live;
    this.box.dataset["state"] = running ? "running" : this.#failed ? "failed" : "done";
    this.#label.textContent = running ? this.#latest || "Working…" : `Used ${this.#total === 1 ? "1 tool" : `${this.#total} tools`}`;
    const bits = running ? [`${this.#total} ${this.#total === 1 ? "step" : "steps"}`] : [];
    if (this.#failed) bits.push(`${this.#failed} failed`);
    this.#count.textContent = bits.join(" · ");
    this.#count.hidden = bits.length === 0;
  }
}

function prettyToolText(value: string): string {
  try { return JSON.stringify(JSON.parse(value), null, 2) ?? value; } catch { return value; }
}

/** Plain wording for Phoenix's own browser tools, so a step reads as an action, not a function name. */
const BROWSER_STEPS: Record<string, string> = {
  browser_tabs: "Checking open tabs", browser_read: "Reading the page", browser_snapshot: "Looking at the page's controls",
  browser_screenshot: "Taking a screenshot", browser_click: "Clicking", browser_type: "Typing", browser_fill: "Filling in a field",
  browser_select: "Choosing an option", browser_press: "Pressing a key", browser_scroll: "Scrolling", browser_drag: "Dragging",
  browser_wait: "Waiting for the page", browser_navigate: "Opening a page", browser_open_tab: "Opening a tab",
  browser_close_tab: "Closing a tab", browser_switch_tab: "Switching tab", browser_back: "Going back",
  browser_forward: "Going forward", browser_reload: "Reloading the page", browser_copy: "Copying", browser_paste: "Pasting",
};

function stepTitle(ev: ChatEvent): string {
  return (/browser/i.test(ev.server ?? "") && ev.toolName && BROWSER_STEPS[ev.toolName]) || ev.text || ev.toolName || "Working…";
}

const IDEAS: readonly [string, IconName][] = [
  ["Summarise this page", "page"],
  ["What are the key points here?", "list"],
  ["Draft a short reply to this", "pen"],
];

export class PhChat extends HTMLElement {
  readonly #logWrap = el("div", "chat-log-wrap");
  readonly #log = el("div", "chat-log");
  readonly #scrollLatest = el("button", "chat-scroll-latest");
  readonly #empty = el("div", "chat-empty");
  readonly #processing = el("div", "chat-processing");
  readonly #processingText = el("span");
  readonly #input = el("textarea", "chat-input");
  readonly #commandMenu = el("div", "slash-menu");
  readonly #send = el("button", "send-btn");
  readonly #share = el("button", "share-chip");
  readonly #newChat = el("button", "chat-new");
  readonly #title = el("span", "chat-title", "New chat");
  readonly #chatHistory = el("details", "chat-history");
  readonly #chatHistoryCount = el("span", "chat-history-count", "0");
  #chatItems: SavedChatSummary[] = [];
  #currentChat = "";
  readonly #closeHistory = (e: Event): void => {
    if (!this.#chatHistory.open) return;
    if (e instanceof KeyboardEvent) {
      if (e.key !== "Escape") return;
      this.#chatHistory.open = false;
      this.#chatHistory.querySelector<HTMLElement>("summary")?.focus();
    } else if (!this.#chatHistory.contains(e.target as Node | null)) {
      this.#chatHistory.open = false;
    }
  };
  readonly #chatHistoryList = el("div", "chat-history-list");
  #shareOn = false;
  #running = false;
  #commands: SlashCommandDefinition[] = [];
  readonly #toolCalls = new Map<string, ToolCardState>();
  #titled = false; // the bar's title has been taken from the first message
  #steps: StepGroup | null = null; // the folded run of tool calls currently being added to
  #activeCommand = 0;
  #current: Msg | null = null;
  #scrollFrame = 0;
  #scrollDirty = false;
  readonly #syncScrollLatest = (): void => {
    const remaining = this.#log.scrollHeight - this.#log.scrollTop - this.#log.clientHeight;
    const threshold = Math.max(120, Math.min(200, this.#log.clientHeight * 0.25));
    this.#scrollLatest.hidden = remaining < threshold;
  };
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
    this.#log.addEventListener("scroll", this.#syncScrollLatest, { passive: true });
    this.#scrollLatest.type = "button";
    this.#scrollLatest.hidden = true;
    this.#scrollLatest.setAttribute("aria-label", "Scroll to latest message");
    this.#scrollLatest.title = "Scroll to latest message";
    this.#scrollLatest.append(icon("chevronDown", 14), el("span", undefined, "Latest"));
    this.#scrollLatest.addEventListener("click", () => this.scrollToLatest(), { passive: true });
    this.#processing.setAttribute("role", "status");
    this.#processing.setAttribute("aria-live", "polite");
    this.#processing.hidden = true;
    this.#processing.append(el("span", "chat-spinner"), this.#processingText);

    // The empty state teaches the one non-obvious feature: sharing the open tab.
    const ideas = el("div", "chat-ideas");
    for (const [text, glyph] of IDEAS) {
      const b = el("button", "chat-idea");
      b.type = "button";
      const mark = el("span", "chat-idea-mark");
      mark.append(icon(glyph, 16));
      b.append(mark, el("span", "chat-idea-text", text), icon("chevronRight", 14));
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
      el("p", undefined, "Give Phoenix a task. It reads the page, clicks and types for you, and shows every step."),
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
    this.#newChat.disabled = true; // nothing to leave until the first message
    this.#newChat.title = "New chat";
    this.#newChat.setAttribute("aria-label", "New chat");
    this.#newChat.append(icon("compose", 18));
    const trigger = el("summary", "chat-history-trigger");
    trigger.title = "Switch session";
    const mark = el("span", "chat-history-icon");
    mark.append(icon("chat", 16));
    const copy = el("span", "chat-history-copy");
    copy.append(el("span", "chat-history-kicker", "Session"), this.#title);
    const chevron = icon("chevronDown", 14);
    chevron.classList.add("chat-history-chevron");
    trigger.append(mark, copy, this.#chatHistoryCount, chevron);
    const pop = el("div", "chat-history-pop");
    pop.append(
      el("p", "chat-history-head", "Switch session"),
      el("p", "chat-history-note", "Stop the current reply to switch sessions."),
      this.#chatHistoryList,
    );
    this.#chatHistory.append(trigger, pop);
    const head = el("div", "chat-head");
    head.append(this.#chatHistory, this.#newChat);
    this.#logWrap.append(this.#log, this.#scrollLatest);
    this.append(head, this.#logWrap, wrap);
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
    document.addEventListener("pointerdown", this.#closeHistory, { passive: true });
    document.addEventListener("keydown", this.#closeHistory);
  }

  disconnectedCallback(): void {
    document.removeEventListener("pointerdown", this.#closeHistory);
    document.removeEventListener("keydown", this.#closeHistory);
    this.#inputObserver.disconnect();
    this.#inputWidth = 0;
  }

  set events(list: ChatEvent[]) {
    for (const ev of list) this.#apply(ev);
    if (list.length) this.scrollToLatest();
  }

  scrollToLatest(): void {
    this.#scrollDirty = true;
    if (this.#scrollFrame) return;

    // content-visibility can expand a message after scrolling reveals its real height.
    const follow = (): void => {
      this.#scrollFrame = 0;
      if (!this.#log.clientHeight) return;
      if (this.#scrollDirty) {
        this.#current?.flush();
        this.#scrollDirty = false;
      }
      this.#log.scrollTop = this.#log.scrollHeight;
      this.#syncScrollLatest();
      this.#scrollFrame = requestAnimationFrame(() => {
        this.#scrollFrame = 0;
        if (this.#log.clientHeight && (this.#scrollDirty || this.#log.scrollHeight - this.#log.scrollTop - this.#log.clientHeight > 1)) {
          this.#scrollFrame = requestAnimationFrame(follow);
        }
      });
    };

    this.#scrollFrame = requestAnimationFrame(follow);
  }

  set chats(value: SavedChatList | SavedChatSummary[]) {
    const list = Array.isArray(value) ? value : value.items;
    this.#currentChat = Array.isArray(value) ? "" : value.current;
    this.#chatItems = list;
    this.#chatHistoryCount.textContent = String(list.length);
    this.#chatHistoryCount.title = `${list.length} saved ${list.length === 1 ? "session" : "sessions"}`;
    this.#chatHistoryList.replaceChildren();
    if (!list.length) {
      this.#chatHistoryList.append(el("p", "chat-history-empty", "Sessions you start will be saved here, so you can come back to them."));
      return;
    }
    for (const chat of list) {
      const current = chat.id === this.#currentChat;
      const button = el("button", "chat-history-item");
      button.type = "button";
      if (current) button.setAttribute("aria-current", "true");
      const main = el("span", "chat-history-main");
      const parts = [when(chat.updatedAt), chat.messages ? `${chat.messages} ${chat.messages === 1 ? "message" : "messages"}` : ""].filter(Boolean);
      main.append(el("span", "chat-history-title", chat.title), el("span", "chat-history-meta", parts.join(" · ")));
      button.append(main);
      if (current) button.append(el("span", "chat-history-now", "Current"));
      button.disabled = this.#running;
      button.addEventListener("click", () => {
        if (this.#running) return;
        this.#chatHistory.open = false;
        if (!current) send("g", "restore", chat.id);
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
    this.scrollToLatest();
  }

  #setRunning(on: boolean): void {
    this.#running = on;
    this.#log.setAttribute("aria-busy", String(on));
    this.#chatHistory.dataset["running"] = String(on);
    this.#chatHistoryList.querySelectorAll<HTMLButtonElement>(".chat-history-item").forEach((button) => { button.disabled = on; });
    this.#syncSend();
    if (!this.#commandMenu.hidden) this.#renderCommandMenu();
  }

  /** The bar names the chat after what the user first asked. */
  #setTitle(text: string): void {
    const t = text.replace(/\s+/g, " ").trim();
    this.#title.textContent = t ? (t.length > 60 ? `${t.slice(0, 59)}…` : t) : "New chat";
  }

  #message(who: string, cls: string, markdown = false): Msg {
    this.#empty.hidden = true;
    this.#newChat.disabled = false;
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
    this.#endSteps();
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
        this.#endSteps();
        this.#empty.hidden = false;
        this.#newChat.disabled = true;
        this.#current = null;
        this.#lastUser = null;
        this.#titled = false;
        this.#setTitle("");
        this.#setRunning(false);
        break;
      case "restore": {
        this.#log.replaceChildren(this.#empty);
        this.#processing.hidden = true;
        this.#toolCalls.clear();
        this.#endSteps();
        this.#empty.hidden = false;
        this.#newChat.disabled = true;
        this.#current = null;
        this.#lastUser = null;
        this.#who = "Agent";
        this.#titled = false;
        this.#setTitle("");
        for (const message of ev.messages ?? []) {
          if (message.role === "user") {
            if (!this.#titled) {
              this.#titled = true;
              this.#setTitle(message.content);
            }
            const m = this.#message("You", "chat-user");
            m.set(message.content);
            this.#lastUser = m.meta;
          } else if (message.role === "assistant") {
            const who = [message.provider, message.model].filter(Boolean).join(" · ") || "Agent";
            const m = this.#message(who, "chat-ai", true);
            m.append(message.content);
            m.flush();
            m.meta.title = who;
          }
        }
        this.#setRunning(false);
        break;
      }
      case "user": {
        this.#processing.hidden = true;
        this.#toolCalls.clear();
        this.#endSteps();
        if (!this.#titled) {
          this.#titled = true;
          this.#setTitle(ev.text ?? "");
        }
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
          const group = this.#stepGroup();
          group.start(ev.text ?? "Working…");
          group.finish(false);
          group.body.append(t);
          break;
        }
        const card = el("details", "chat-tool-card") as HTMLDetailsElement;
        const summary = document.createElement("summary");
        const status = el("span", "chat-tool-status", "Running");
        const chevron = icon("chevronRight", 14);
        chevron.classList.add("chat-tool-chevron");
        summary.append(icon("bolt", 13), el("span", "chat-tool-title", stepTitle(ev)), status, chevron);
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
        const group = this.#stepGroup();
        group.start(stepTitle(ev));
        group.body.append(card);
        this.#toolCalls.set(ev.toolId, { group, card, status, output, outputSection });
        break;
      }
      case "tool-result": {
        const call = ev.toolId ? this.#toolCalls.get(ev.toolId) : undefined;
        if (call) {
          call.group.finish(!!ev.toolError);
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
          this.#endSteps(); // tools after this text start a new folded run
          this.#current = this.#message(this.#who, "chat-ai", true);
          this.#current.meta.title = this.#who;
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
          // One short line; the model name and exact token counts are in the tooltip.
          const tokens = (ev.in ?? 0) + (ev.out ?? 0);
          const bits = [ev.ms ? `${(ev.ms / 1000).toFixed(1)} s` : "", tokens ? `${tokens >= 1000 ? `${(tokens / 1000).toFixed(1)}k` : tokens} tokens` : "", ev.cost ? `$${ev.cost.toFixed(3)}` : ""];
          c.meta.textContent = bits.filter(Boolean).join(" · ");
          c.meta.title = [c.who, ev.out ? `${ev.in ?? 0} in, ${ev.out} out` : ""].filter(Boolean).join(" · ");
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

  /** Closes the current folded run (its row stops saying "working") so the next tool call starts a new one. */
  #endSteps(): void {
    this.#steps?.settle();
    this.#steps = null;
  }

  /** The folded run tool calls are added to; a new one starts after any message or card. */
  #stepGroup(): StepGroup {
    if (!this.#steps) {
      this.#steps = new StepGroup();
      this.#log.append(this.#steps.box);
    }
    return this.#steps;
  }

  #finishToolCalls(label: string): void {
    this.#steps?.settle();
    this.#endSteps();
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
