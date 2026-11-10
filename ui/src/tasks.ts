// <ph-tasks>: the Tasks page. Tasks are saved automations the agent runs on its own, each in a
// session of its own. Go owns the data and the runs; this element lists, edits and starts them.
// Everything the user or the agent wrote reaches the page through .value or textContent only.
import { el } from "./dom";
import { icon, isIcon, type IconName } from "./icons";
import { send } from "./protocol";

type Kind = "manual" | "interval" | "daily" | "weekdays";
type Status = "ok" | "blocked" | "failed" | "stopped";

interface Run {
  at: number;
  status: Status;
  result?: string | undefined;
  ms?: number | undefined;
  chat?: string | undefined;
}

interface Task {
  id: string;
  name: string;
  prompt: string;
  goal?: boolean | undefined;
  extra?: string | undefined;
  icon?: string | undefined;
  kind: Kind;
  every?: number | undefined;
  at?: string | undefined;
  enabled: boolean;
  limit?: boolean | undefined;
  tools?: string[] | undefined;
  notify?: boolean | undefined;
  schedule?: string | undefined;
  next?: number | undefined;
  lastRun?: number | undefined;
  lastStatus?: Status | undefined;
  lastResult?: string | undefined;
  runs?: number | undefined;
  history?: Run[] | undefined;
}

interface Data {
  tasks: Task[];
  servers?: string[] | undefined;
  desktop?: boolean | undefined;
  running?: string | undefined;
  waiting?: string | undefined;
  instructionBudget?: number | undefined;
  promptLimit?: number | undefined;
  priorityRulesLimit?: number | undefined;
}

interface Outcome {
  ok?: boolean;
  text?: string;
  error?: string;
}

interface Idea {
  icon: string;
  name: string;
  blurb: string;
  prompt: string;
  kind: Kind;
  at?: string;
  tools?: string[];
}

/** Starting points. Each fills the editor; the user adjusts the wording, schedule and tools. */
const IDEAS: Idea[] = [
  { icon: "briefcase", name: "Morning job sweep", blurb: "Find new matching listings and apply to the best ones.", kind: "weekdays", at: "09:00", tools: ["browser", "files"],
    prompt: "Open my saved job searches and job alerts, find listings that match my profile and are new since the last run, and apply to the best matches. Skip any I already applied to." },
  { icon: "mail", name: "Inbox triage", blurb: "Read what arrived overnight and tell me what needs a reply.", kind: "daily", at: "08:30", tools: ["browser"],
    prompt: "Open my email in the browser, read what arrived since yesterday, and report how many messages need a reply and from whom. Do not send or delete anything." },
  { icon: "news", name: "Morning brief", blurb: "Five headlines that matter to me, saved as a note.", kind: "weekdays", at: "07:30", tools: ["browser", "files"],
    prompt: "Open three reputable news sites in my field, pick the five stories that matter most to me, and save a short brief in the file library under briefs/ named with today's date." },
  { icon: "trend", name: "Price watch", blurb: "Check a product page and report any price change.", kind: "daily", at: "12:00", tools: ["browser"],
    prompt: "Open the product page I name below, read the current price and stock, and report whether it changed since the last run.\n\nPage: " },
  { icon: "search", name: "Research to a note", blurb: "Read several sources on a topic and save a brief.", kind: "manual", tools: ["browser", "files"],
    prompt: "Research the topic below across at least five good sources, then save a one-page brief with the key facts and links in the file library under research/.\n\nTopic: " },
  { icon: "calendar", name: "Application tracker", blurb: "Check where my job applications stand.", kind: "daily", at: "18:00", tools: ["browser", "files"],
    prompt: "List the applications recorded so far, open the status page or email for each one that is still open, and report any that moved (interview, rejection, offer)." },
  { icon: "doc", name: "Fill a form for me", blurb: "Complete a web form with my saved details.", kind: "manual", tools: ["browser", "files"],
    prompt: "Open the form below, fill every field from my profile and memory, attach what it needs from my files, and submit it.\n\nForm: " },
  { icon: "layers", name: "Summarize my open tabs", blurb: "Turn the tabs I left open into one tidy note.", kind: "manual", tools: ["browser", "files"],
    prompt: "Look at all my open tabs, summarize each in one line, group them by theme, and save the result as a note in the file library under notes/." },
];

const ICONS: IconName[] = ["bolt", "briefcase", "mail", "news", "trend", "search", "calendar", "doc", "layers", "cart", "plane", "home", "cap", "bulb", "bell", "broom", "chart", "bot"];

/** A task icon, falling back to the bolt for anything this set does not draw (older saved tasks). */
function glyph(name: string | undefined, size = 16): SVGSVGElement {
  return icon(isIcon(name) ? name : "bolt", size);
}

const STATUS_TEXT: Record<string, string> = { ok: "Done", blocked: "Blocked", failed: "Failed", stopped: "Stopped" };

export function ago(unix: number, now = Date.now()): string {
  const s = Math.max(0, Math.round((now - unix * 1000) / 1000));
  if (s < 60) return "just now";
  if (s < 3600) return `${Math.floor(s / 60)} min ago`;
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`;
  return `${Math.floor(s / 86400)} d ago`;
}

function when(unix: number): string {
  const d = new Date(unix * 1000);
  const today = new Date();
  const tomorrow = new Date(today.getTime() + 86400000);
  const time = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(d);
  if (d.toDateString() === today.toDateString()) return `today ${time}`;
  if (d.toDateString() === tomorrow.toDateString()) return `tomorrow ${time}`;
  return `${new Intl.DateTimeFormat(undefined, { weekday: "short" }).format(d)} ${time}`;
}

function seconds(ms: number): string {
  const s = Math.round(ms / 1000);
  return s < 60 ? `${s} s` : `${Math.floor(s / 60)} min ${s % 60} s`;
}

/** Hue for a task's tile, stable per task so a card keeps its colour. */
function hue(s: string): number {
  let h = 0;
  for (const c of s) h = (h * 31 + c.codePointAt(0)!) % 360;
  return h;
}

const TOOL_LABEL: Record<string, string> = { browser: "Browser", files: "Files", desktop: "Computer use" };

function toolChips(t: Task): string[] {
  if (!t.limit) return ["Everything on"];
  const names = (t.tools ?? []).map((id) => TOOL_LABEL[id] ?? (id.startsWith("mcp:") ? id.slice(4) : id));
  return names.length ? names : ["No tools"];
}

/** Hover text (the browser's title tooltip) for every control that is an icon or a short word. */
const TIPS: Record<string, string> = {
  surprise: "Open a random idea",
  new: "Create a task from scratch",
  "filter:all": "Show every task",
  "filter:scheduled": "Show tasks that run by themselves",
  "filter:manual": "Show tasks you start yourself",
  "filter:attention": "Show tasks whose last run was blocked or failed",
  run: "Run now, in a new session",
  stop: "Stop this run",
  watch: "Watch the run in the chat",
  edit: "Change this task",
  duplicate: "Make a copy of this task",
  delete: "Delete this task for good",
  close: "Close the details",
  session: "Open the chat of that run",
  "toggle:on": "Pause the schedule. Nothing runs until you resume it.",
  "toggle:off": "Resume the schedule",
  "step:ideas": "Start from a ready-made idea",
  "step:what": "Name the task and say what to do",
  "step:when": "Choose when it runs",
  "step:tools": "Choose what the agent may use",
  "kind:manual": "Runs only when you press Run",
  "kind:interval": "Repeats every few minutes or hours",
  "kind:daily": "Runs once a day at a time you pick",
  "kind:weekdays": "Runs Monday to Friday at a time you pick",
  "save-run": "Save the task and start it now",
  cancel: "Close without saving",
  save: "Save the task",
};

const TOOL_TIP: Record<string, string> = {
  browser: "Open pages, click, type and read in your browser",
  files: "Read and save files in your Phoenix library",
  desktop: "Control other apps on your PC",
};

const ICON_TIP: Record<string, string> = {
  bolt: "Bolt", briefcase: "Briefcase", mail: "Mail", news: "News", trend: "Trend", search: "Search", calendar: "Calendar",
  doc: "Document", layers: "Layers", cart: "Shopping", plane: "Travel", home: "Home", cap: "Learning", bulb: "Idea",
  bell: "Reminder", broom: "Cleanup", chart: "Chart", bot: "Robot",
};

type Filter = "all" | "scheduled" | "manual" | "attention";

class PhTasks extends HTMLElement {
  #data: Data = { tasks: [] };
  #filter: Filter = "all";
  #query = "";
  #open = ""; // task shown in the details drawer
  #confirm = ""; // a delete waiting for its second click
  #editing: Task | null = null;
  #icon: string = "bolt";

  readonly #head = el("header", "tp-head");
  readonly #bar = el("div", "tp-bar");
  readonly #body = el("div", "tp-body");
  readonly #drawer = el("aside", "tp-drawer");
  readonly #dialog = el("dialog", "tp-dialog");
  readonly #form = el("form", "tp-form");

  // editor fields
  readonly #name = el("input", "field field-wide");
  readonly #prompt = el("textarea", "tp-text");
  readonly #goal = el("input");
  readonly #extra = el("textarea", "tp-text tp-text-sm");
  readonly #instructionBudget = el("p", "tp-note");
  readonly #kind = el("div", "tp-seg");
  readonly #every = el("input", "field");
  readonly #unit = el("select", "field");
  readonly #at = el("input", "field");
  readonly #all = el("input");
  readonly #picks = el("div", "tp-picks");
  readonly #notify = el("input");
  readonly #icons = el("div", "tp-icons");
  readonly #side = el("nav", "tp-side");
  readonly #panes = el("div", "tp-panes");
  readonly #stepVal: Record<string, HTMLElement> = {};
  readonly #err = el("p", "msg");
  #kindValue: Kind = "manual";

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#buildEditor();
    this.#drawer.hidden = true;
    this.append(this.#head, this.#bar, this.#body, this.#drawer, this.#dialog);
    this.#body.addEventListener("click", (e) => this.#click(e), { passive: true });
    this.#head.addEventListener("click", (e) => this.#click(e), { passive: true });
    this.#drawer.addEventListener("click", (e) => this.#click(e), { passive: true });
    this.addEventListener("notification-open", (e) => { this.#open = (e as CustomEvent<string>).detail; this.#renderDrawer(); });
    this.#dialog.addEventListener("click", (e) => this.#click(e), { passive: true });
    this.#dialog.addEventListener("close", () => { this.#editing = null; });
    this.addEventListener("keydown", (e) => { if (e.key === "Escape" && !this.#drawer.hidden && !this.#dialog.open) this.#closeDrawer(); });
    this.#render();
  }

  set data(d: Data) {
    this.#data = { tasks: Array.isArray(d?.tasks) ? d.tasks : [], servers: d?.servers ?? [], desktop: d?.desktop, running: d?.running, waiting: d?.waiting };
    this.#data.instructionBudget = d?.instructionBudget ?? 1500;
    this.#data.promptLimit = d?.promptLimit ?? 4000;
    this.#data.priorityRulesLimit = d?.priorityRulesLimit ?? 2000;
    this.#prompt.maxLength = this.#data.promptLimit;
    this.#extra.maxLength = this.#data.priorityRulesLimit;
    this.#render();
  }

  set result(r: Outcome) {
    if (!this.#editing) return;
    if (r.error) { this.#err.textContent = r.error; return; }
    this.#dialog.close();
  }

  // ── Editor ───────────────────────────────────────────────────────────────

  #buildEditor(): void {
    const field = (label: string, control: HTMLElement): HTMLElement => {
      const f = el("div", "tp-field");
      f.append(el("span", "tp-label", label), control);
      return f;
    };
    const pane = (id: string, title: string, ...kids: HTMLElement[]): HTMLElement => {
      const s = el("section", "tp-pane");
      s.dataset["step"] = id;
      s.append(el("h3", undefined, title), ...kids);
      return s;
    };

    this.#name.placeholder = "Name this task";
    this.#name.maxLength = 60;
    this.#name.setAttribute("aria-label", "Task name");
    this.#name.title = "Shown on the task card";
    this.#prompt.placeholder = "Where to go, what to do, when it is done.";
    this.#prompt.maxLength = this.#data.promptLimit ?? 4000;
    this.#prompt.setAttribute("aria-label", "Instructions");
    this.#prompt.title = "What the agent should do. It will not ask questions while it runs.";
    this.#goal.type = "checkbox";
    this.#goal.title = "Keep the agent working across turns until it verifies the goal is complete";
    this.#goal.addEventListener("change", () => this.#syncGoal(), { passive: true });
    const goalLabel = el("label", "tp-check");
    goalLabel.append(this.#goal, el("span", "tp-check-text", "Keep working until the goal is complete"));
    this.#extra.placeholder = "Hard limits, exclusions, deadlines";
    this.#extra.maxLength = this.#data.priorityRulesLimit ?? 2000;
    this.#extra.setAttribute("aria-label", "Priority rules");
    this.#extra.title = "Hard constraints that take priority over the task goal";
    this.#instructionBudget.setAttribute("aria-live", "polite");

    for (const ic of ICONS) {
      const b = el("button", "tp-icon");
      b.append(glyph(ic, 22));
      b.type = "button";
      b.dataset["op"] = "icon";
      b.dataset["id"] = ic;
      b.setAttribute("aria-label", `Icon: ${ICON_TIP[ic] ?? ic}`);
      b.title = ICON_TIP[ic] ?? ic;
      this.#icons.append(b);
    }

    const ideaGrid = el("div", "tp-grid");
    IDEAS.forEach((idea, n) => ideaGrid.append(this.#ideaCard(idea, n, "idea")));

    const kinds: [Kind, string][] = [["manual", "On demand"], ["interval", "Repeat"], ["daily", "Daily"], ["weekdays", "Weekdays"]];
    this.#kind.setAttribute("role", "radiogroup");
    this.#kind.setAttribute("aria-label", "When");
    for (const [v, l] of kinds) {
      const b = el("button", "tp-seg-btn", l);
      b.type = "button";
      b.setAttribute("role", "radio");
      b.dataset["op"] = "kind";
      b.dataset["id"] = v;
      b.title = TIPS[`kind:${v}`] ?? "";
      this.#kind.append(b);
    }
    this.#every.type = "number";
    this.#every.min = "1";
    this.#every.setAttribute("aria-label", "Repeat every");
    this.#every.title = "How often it repeats";
    for (const [v, l] of [["60", "hours"], ["1", "minutes"]] as const) {
      const o = el("option", undefined, l);
      o.value = v;
      this.#unit.append(o);
    }
    this.#unit.setAttribute("aria-label", "Unit");
    this.#at.type = "time";
    this.#at.setAttribute("aria-label", "Time of day");
    this.#at.title = "Time of day, in your local time";
    const whenRow = el("div", "tp-when");
    whenRow.append(el("span", "tp-when-every", "every"), this.#every, this.#unit, el("span", "tp-when-at", "at"), this.#at);

    this.#all.type = "checkbox";
    const allLabel = el("label", "tp-check");
    allLabel.append(this.#all, el("span", "tp-check-text", "Everything that is switched on"));
    allLabel.title = "Give the task every tool you have switched on";
    this.#all.addEventListener("change", () => { this.#picks.hidden = this.#all.checked; }, { passive: true });
    this.#notify.type = "checkbox";
    const notifyLabel = el("label", "tp-check");
    notifyLabel.append(this.#notify, el("span", "tp-check-text", "Alert me when it finishes"));
    notifyLabel.title = "Flash the taskbar when a run ends, if Phoenix is not in front";

    this.#panes.append(
      pane("ideas", "Ideas", ideaGrid),
      pane("what", "What to do", field("Icon", this.#icons), field("Name", this.#name), field("Task instructions", this.#prompt), goalLabel, field("Priority rules (optional)", this.#extra), this.#instructionBudget),
      pane("when", "When", this.#kind, whenRow),
      pane("tools", "Tools", allLabel, this.#picks, notifyLabel),
    );

    const steps: [string, string, IconName][] = [["ideas", "Ideas", "bulb"], ["what", "What to do", "pen"], ["when", "When", "clock"], ["tools", "Tools", "layers"]];
    this.#side.setAttribute("role", "tablist");
    this.#side.setAttribute("aria-label", "Task settings");
    for (const [id, label, g] of steps) {
      const b = el("button", "tp-step");
      b.type = "button";
      b.setAttribute("role", "tab");
      b.dataset["op"] = "step";
      b.dataset["id"] = id;
      b.title = TIPS[`step:${id}`] ?? "";
      const copy = el("span", "tp-step-copy");
      const val = el("span", "tp-step-val");
      this.#stepVal[id] = val;
      copy.append(el("strong", undefined, label), val);
      b.append(icon(g, 20), copy);
      this.#side.append(b);
    }

    const save = el("button", "btn btn-primary", "Save");
    save.type = "submit";
    save.title = TIPS["save"] ?? "";
    const saveRun = el("button", "btn", "Save and run");
    saveRun.type = "button";
    saveRun.dataset["op"] = "save-run";
    saveRun.title = TIPS["save-run"] ?? "";
    const cancel = el("button", "btn btn-quiet", "Cancel");
    cancel.type = "button";
    cancel.dataset["op"] = "cancel";
    cancel.title = TIPS["cancel"] ?? "";
    const foot = el("div", "tp-foot");
    foot.append(this.#err, cancel, saveRun, save);

    const right = el("div", "tp-right");
    right.append(this.#panes, foot);
    this.#form.append(this.#side, right);
    this.#form.addEventListener("submit", (e) => { e.preventDefault(); this.#save(false); });
    this.#form.addEventListener("input", () => { this.#syncSide(); this.#syncInstructionBudget(); }, { passive: true });
    this.#form.addEventListener("change", () => { this.#syncSide(); this.#syncInstructionBudget(); }, { passive: true });
    this.#dialog.append(this.#form);
  }

  #ideaCard(i: Idea, n: number, op: string): HTMLElement {
    const c = el("button", "tp-idea");
    c.type = "button";
    c.dataset["op"] = op;
    c.dataset["id"] = String(n);
    const tile = el("span", "tp-tile");
    tile.append(glyph(i.icon, 24));
    tile.style.setProperty("--h", String(hue(i.name)));
    const copy = el("span", "tp-idea-copy");
    copy.append(el("strong", undefined, i.name), el("span", undefined, i.blurb), el("em", undefined, i.kind === "manual" ? "On demand" : i.kind === "daily" ? `Daily ${i.at}` : `Weekdays ${i.at}`));
    c.append(tile, copy);
    c.style.animationDelay = `${n * 40}ms`;
    return c;
  }

  #goStep(id: string): void {
    this.#side.querySelectorAll<HTMLElement>(".tp-step").forEach((b) => b.setAttribute("aria-selected", String(b.dataset["id"] === id)));
    this.#panes.querySelectorAll<HTMLElement>(".tp-pane").forEach((p) => { p.hidden = p.dataset["step"] !== id; });
    this.#panes.scrollTop = 0;
  }

  /** The one-line value under each side item, so the user sees the state without opening it. */
  #syncSide(): void {
    const k = this.#kindValue;
    const picked = this.#picks.querySelectorAll<HTMLInputElement>("input:checked").length;
    this.#stepVal["what"]!.textContent = this.#name.value.trim() || "Untitled";
    this.#stepVal["when"]!.textContent = k === "manual" ? "On demand" : k === "interval" ? `Every ${this.#every.value} ${this.#unit.selectedOptions[0]?.text ?? ""}` : `${k === "daily" ? "Daily" : "Weekdays"} ${this.#at.value}`;
    this.#stepVal["tools"]!.textContent = this.#all.checked ? "Everything" : `${picked} picked`;
    this.#stepVal["ideas"]!.textContent = "Start from one";
  }

  /** A conservative shared estimate for the text that is sent on every task run. */
  #syncInstructionBudget(): void {
    const runes = (s: string): number => Array.from(s).length;
    const tokens = Math.ceil(runes(this.#prompt.value) / 4) + Math.ceil(runes(this.#extra.value) / 4);
    const limit = this.#data.instructionBudget ?? 1500;
    this.#instructionBudget.textContent = `Your text: about ${tokens.toLocaleString()} / ${limit.toLocaleString()} tokens. Priority rules outrank the task goal; Phoenix rules and tool limits come first.`;
    this.#instructionBudget.dataset["over"] = String(tokens > limit);
  }

  #openEditor(t: Task): void {
    this.#editing = t;
    this.#icon = isIcon(t.icon) ? t.icon : "bolt";
    this.#name.value = t.name;
    this.#prompt.value = t.prompt;
    this.#goal.checked = t.goal ?? false;
    this.#extra.value = t.extra ?? "";
    this.#setKind(t.kind);
    this.#syncGoal();
    const mins = t.every ?? 60;
    const hours = mins % 60 === 0;
    this.#unit.value = hours ? "60" : "1";
    this.#every.value = String(hours ? mins / 60 : mins);
    this.#at.value = t.at || "09:00";
    this.#all.checked = !t.limit;
    this.#fillPicks(new Set(t.tools ?? ["browser", "files"]));
    this.#picks.hidden = this.#all.checked;
    this.#notify.checked = t.notify ?? true;
    this.#side.querySelector<HTMLElement>('[data-id="ideas"]')!.hidden = t.id !== "" || t.prompt !== "";
    this.#err.textContent = "";
    this.#syncInstructionBudget();
    this.#markIcons();
    this.#goStep(t.id === "" && t.prompt === "" ? "ideas" : "what");
    this.#syncSide();
    this.#syncInstructionBudget();
    this.#dialog.showModal();
  }

  #fillPicks(on: Set<string>): void {
    this.#picks.replaceChildren();
    const row = (id: string, label: string, glyphName: IconName, note = "", disabled = false): void => {
      const l = el("label", "tp-check");
      const c = el("input");
      c.type = "checkbox";
      c.value = id;
      c.checked = on.has(id) && !disabled;
      c.disabled = disabled;
      l.title = id.startsWith("mcp:") ? `Tools from the ${label} server` : (disabled ? "Switch computer use on in Settings first" : (TOOL_TIP[id] ?? label));
      const text = el("span", "tp-check-text");
      text.append(icon(glyphName, 16), label);
      l.append(c, text);
      if (note) l.append(el("em", undefined, note));
      this.#picks.append(l);
    };
    row("browser", "Browser", "globe");
    row("files", "Files", "folder");
    row("desktop", "Computer use", "monitor", this.#data.desktop ? "" : "off in Settings", !this.#data.desktop);
    for (const s of this.#data.servers ?? []) row(`mcp:${s}`, s, "plug");
    if ((this.#data.servers ?? []).length === 0) this.#picks.append(el("p", "tp-note", "No MCP servers on."));
  }

  #setKind(k: Kind): void {
    this.#kindValue = k;
    this.#kind.querySelectorAll<HTMLElement>(".tp-seg-btn").forEach((b) => b.setAttribute("aria-checked", String(b.dataset["id"] === k)));
    this.#every.hidden = this.#unit.hidden = k !== "interval";
    this.#at.hidden = k !== "daily" && k !== "weekdays";
    this.#form.querySelector<HTMLElement>(".tp-when-every")!.hidden = k !== "interval";
    this.#form.querySelector<HTMLElement>(".tp-when-at")!.hidden = this.#at.hidden;
    this.#form.querySelector<HTMLElement>(".tp-when")!.hidden = k === "manual";
  }

  #syncGoal(): void {
    if (this.#goal.checked) this.#setKind("manual");
    this.#kind.hidden = this.#goal.checked;
    this.#form.querySelector<HTMLElement>(".tp-when")!.hidden = this.#goal.checked || this.#kindValue === "manual";
  }

  #markIcons(): void {
    this.#icons.querySelectorAll<HTMLElement>(".tp-icon").forEach((b) => b.setAttribute("aria-pressed", String(b.dataset["id"] === this.#icon)));
  }

  #save(run: boolean): void {
    const base = this.#editing;
    if (!base) return;
    if (!this.#prompt.value.trim()) { this.#err.textContent = "Describe the job first."; this.#prompt.focus(); return; }
    const k = this.#goal.checked ? "manual" : this.#kindValue;
    const tools = Array.from(this.#picks.querySelectorAll<HTMLInputElement>("input:checked"), (c) => c.value);
    const task: Partial<Task> = {
      id: base.id,
      name: this.#name.value,
      prompt: this.#prompt.value,
      goal: this.#goal.checked,
      extra: this.#extra.value,
      icon: this.#icon,
      kind: k,
      every: k === "interval" ? Math.round(Number(this.#every.value) * Number(this.#unit.value)) : 0,
      at: k === "daily" || k === "weekdays" ? this.#at.value : "",
      limit: !this.#all.checked,
      tools: this.#all.checked ? [] : tools,
      notify: this.#notify.checked,
    };
    this.#err.textContent = "";
    this.#syncInstructionBudget();
    if (this.#instructionBudget.dataset["over"] === "true") {
      this.#err.textContent = "Shorten the task text to about 1,500 tokens before saving.";
      return;
    }
    if (!this.#all.checked && tools.length === 0 && !window.confirm("No tools picked. Save anyway?")) return;
    send("k", "save", JSON.stringify(task), run ? "1" : "0");
  }

  // ── Actions ──────────────────────────────────────────────────────────────

  #click(e: Event): void {
    const b = (e.target as Element | null)?.closest<HTMLElement>("[data-op]");
    if (!b) return;
    const id = b.dataset["id"] ?? "";
    const task = this.#data.tasks.find((t) => t.id === id);
    const blank: Task = { id: "", name: "", prompt: "", kind: "manual", enabled: false, icon: "bolt" };
    switch (b.dataset["op"]) {
      case "new": this.#openEditor(blank); break;
      case "surprise": {
        const i = IDEAS[Math.floor(Math.random() * IDEAS.length)]!;
        this.#openEditor({ ...blank, name: i.name, prompt: i.prompt, icon: i.icon, kind: i.kind, at: i.at, limit: !!i.tools, tools: i.tools ?? [] });
        break;
      }
      case "idea": {
        const i = IDEAS[Number(id)];
        if (!i) break;
        this.#name.value = i.name;
        this.#prompt.value = i.prompt;
        this.#icon = i.icon;
        this.#markIcons();
        this.#setKind(i.kind);
        if (i.at) this.#at.value = i.at;
        this.#all.checked = !i.tools;
        this.#fillPicks(new Set(i.tools ?? []));
        this.#picks.hidden = this.#all.checked;
        this.#extra.value = "";
        this.#syncSide();
        this.#side.querySelector<HTMLElement>('[data-id="ideas"]')!.hidden = false;
        this.#goStep("what");
        this.#prompt.focus();
        break;
      }
      case "idea-new": {
        const i = IDEAS[Number(id)];
        if (i) this.#openEditor({ ...blank, name: i.name, prompt: i.prompt, icon: i.icon, kind: i.kind, at: i.at, limit: !!i.tools, tools: i.tools ?? [] });
        break;
      }
      case "step": this.#goStep(id); break;
      case "icon": this.#icon = id; this.#markIcons(); break;
      case "kind": this.#setKind(id as Kind); break;
      case "cancel": this.#dialog.close(); break;
      case "save-run": this.#save(true); break;
      case "open": this.#open = id; this.#renderDrawer(); break;
      case "close": this.#closeDrawer(); break;
      case "filter": this.#filter = id as Filter; this.#render(); break;
      case "run": send("k", "run", id); break;
      case "stop": send("k", "stop"); break;
      case "toggle": if (task) send("k", "enable", id, task.enabled ? "0" : "1"); break;
      case "edit": if (task) this.#openEditor(task); break;
      case "duplicate": send("k", "duplicate", id); break;
      case "session": send("k", "session", id); break;
      case "delete":
        if (this.#confirm !== id) {
          this.#confirm = id;
          this.#renderDrawer();
          setTimeout(() => { if (this.#confirm === id) { this.#confirm = ""; this.#renderDrawer(); } }, 4000);
        } else {
          this.#confirm = "";
          this.#closeDrawer();
          send("k", "delete", id);
        }
        break;
    }
  }

  #closeDrawer(): void {
    this.#open = "";
    this.#drawer.hidden = true;
    this.dataset["drawer"] = "off";
  }

  // ── Drawing ──────────────────────────────────────────────────────────────

  #button(op: string, label: string, id: string, cls = "btn btn-quiet", glyphName?: IconName): HTMLButtonElement {
    const b = el("button", cls);
    if (glyphName) b.append(icon(glyphName, 16));
    if (label) b.append(label);
    const tip = TIPS[`${op}:${id}`] ?? TIPS[op];
    if (tip) b.title = tip;
    if (!label) b.setAttribute("aria-label", tip ?? op);
    b.type = "button";
    b.dataset["op"] = op;
    b.dataset["id"] = id;
    return b;
  }

  #render(): void {
    if (this.childElementCount === 0) return;
    const { tasks, running, waiting } = this.#data;
    this.#renderHead();
    this.#renderBar();
    this.#body.replaceChildren();
    if (tasks.length === 0) { this.#body.append(this.#gallery()); this.#renderDrawer(); return; }
    const q = this.#query.trim().toLowerCase();
    const shown = tasks.filter((t) => {
      if (q && !`${t.name} ${t.prompt}`.toLowerCase().includes(q)) return false;
      switch (this.#filter) {
        case "scheduled": return t.kind !== "manual";
        case "manual": return t.kind === "manual";
        case "attention": return t.lastStatus === "blocked" || t.lastStatus === "failed";
        default: return true;
      }
    });
    if (shown.length === 0) {
      this.#body.append(el("p", "tp-none", "Nothing here. Try another filter."));
    } else {
      const grid = el("div", "tp-grid");
      for (const t of shown) grid.append(this.#card(t, running === t.id, waiting === t.id));
      this.#body.append(grid);
    }
    this.#renderDrawer();
  }

  #renderHead(): void {
    const { tasks, running } = this.#data;
    this.#head.replaceChildren();
    const live = tasks.filter((t) => t.enabled && t.kind !== "manual").length;
    const runs = tasks.reduce((n, t) => n + (t.runs ?? 0), 0);
    const hist = tasks.flatMap((t) => t.history ?? []);
    const rate = hist.length ? Math.round((hist.filter((r) => r.status === "ok").length / hist.length) * 100) : 0;
    const next = tasks.map((t) => t.next ?? 0).filter((n) => n > 0).sort((a, b) => a - b)[0];

    const title = el("div", "tp-title");
    title.append(
      el("h1", undefined, "Tasks"),
      el("p", undefined, tasks.length === 0
        ? "Jobs the agent does for you."
        : running ? "Working…" : live ? `${live} scheduled${next ? ` · next ${when(next)}` : ""}` : "Nothing scheduled"),
    );
    const stats = el("div", "tp-stats");
    if (tasks.length > 0) {
      for (const [n, l] of [[String(tasks.length), "tasks"], [String(runs), "runs"], [hist.length ? `${rate}%` : "–", "went well"]] as const) {
        const s = el("div", "tp-stat");
        s.append(el("strong", undefined, n), el("span", undefined, l));
        stats.append(s);
      }
    }
    const acts = el("div", "tp-acts");
    acts.append(this.#button("surprise", "Surprise me", "", "btn", "shuffle"), this.#button("new", "New task", "", "btn btn-primary", "plus"));
    this.#head.append(title, stats, acts);
  }

  #renderBar(): void {
    this.#bar.hidden = this.#data.tasks.length === 0;
    if (this.#bar.childElementCount === 0) {
      const pills = el("div", "tp-pills");
      pills.setAttribute("role", "group");
      for (const [v, l] of [["all", "All"], ["scheduled", "Scheduled"], ["manual", "On demand"], ["attention", "Needs attention"]] as const) {
        pills.append(this.#button("filter", l, v, "tp-pill"));
      }
      const search = el("input", "field");
      search.type = "search";
      search.placeholder = "Search tasks";
      search.setAttribute("aria-label", "Search tasks");
      search.addEventListener("input", () => { this.#query = search.value; this.#render(); }, { passive: true });
      this.#bar.append(pills, search);
    }
    this.#bar.querySelectorAll<HTMLElement>(".tp-pill").forEach((p) => p.setAttribute("aria-pressed", String(p.dataset["id"] === this.#filter)));
  }

  #gallery(): HTMLElement {
    const g = el("div", "tp-gallery");
    g.append(el("h2", undefined, "Pick an idea to start"));
    const grid = el("div", "tp-grid");
    IDEAS.forEach((i, n) => grid.append(this.#ideaCard(i, n, "idea-new")));
    g.append(grid);
    return g;
  }

  #card(t: Task, running: boolean, waiting: boolean): HTMLElement {
    const card = el("article", "tp-card");
    card.dataset["state"] = running ? "running" : waiting ? "waiting" : (t.lastStatus ?? "new");
    card.dataset["op"] = "open";
    card.title = `${t.name}: open details`;
    card.dataset["id"] = t.id;
    card.tabIndex = 0;
    card.addEventListener("keydown", (e) => { if (e.key === "Enter" && e.target === card) card.click(); });

    const tile = el("span", "tp-tile");
    tile.append(glyph(t.icon, 24));
    tile.style.setProperty("--h", String(hue(t.id || t.name)));
    const top = el("div", "tp-card-top");
    const names = el("div", "tp-card-names");
    names.append(el("h3", undefined, t.name), el("p", "tp-sched", t.goal ? "Goal · runs until complete" : t.kind === "manual" ? "On demand" : `${t.schedule ?? ""}${t.enabled ? (t.next ? ` · next ${when(t.next)}` : "") : " · paused"}`));
    top.append(tile, names, el("span", "tp-dot"));

    const result = el("p", "tp-result");
    if (running) result.textContent = t.goal ? "Working toward the goal…" : "Running now…";
    else if (waiting) result.textContent = "Waiting for the agent to be free…";
    else if (t.lastRun) result.textContent = `${STATUS_TEXT[t.lastStatus ?? "ok"] ?? ""} ${ago(t.lastRun)}${t.lastResult ? ": " + t.lastResult : ""}`;
    else result.textContent = "Not run yet.";
    result.title = result.textContent;

    const chips = el("div", "tp-chips");
    for (const c of toolChips(t).slice(0, 3)) { const tag = el("span", "tp-tag", c); tag.title = `Tool: ${c}`; chips.append(tag); }

    const foot = el("div", "tp-card-foot");
    if (running) foot.append(this.#button("stop", "Stop", t.id, "btn", "stop"));
    else {
      const run = this.#button("run", waiting ? "Waiting…" : "Run", t.id, "btn btn-primary", waiting ? undefined : "play");
      run.disabled = waiting;
      foot.append(run);
    }
    foot.append(chips);
    card.append(top, result, foot);
    return card;
  }

  #renderDrawer(): void {
    const t = this.#data.tasks.find((x) => x.id === this.#open);
    if (!t) { this.#closeDrawer(); return; }
    this.#drawer.hidden = false;
    this.dataset["drawer"] = "on";
    const running = this.#data.running === t.id;
    const waiting = this.#data.waiting === t.id;
    const d = this.#drawer;
    d.replaceChildren();

    const head = el("div", "tp-d-head");
    const tile = el("span", "tp-tile");
    tile.append(glyph(t.icon, 24));
    tile.style.setProperty("--h", String(hue(t.id || t.name)));
    const names = el("div", "tp-card-names");
    names.append(el("h2", undefined, t.name), el("p", "tp-sched", t.goal ? "Goal · runs until complete" : t.kind === "manual" ? "On demand" : `${t.schedule ?? ""}${t.enabled ? (t.next ? ` · next ${when(t.next)}` : "") : " · paused"}`));
    const close = this.#button("close", "", "", "btn btn-quiet", "close");
    close.setAttribute("aria-label", "Close details");
    head.append(tile, names, close);

    const acts = el("div", "tp-d-acts");
    if (running) acts.append(this.#button("stop", "Stop", t.id, "btn", "stop"));
    else {
      const run = this.#button("run", waiting ? "Waiting…" : "Run now", t.id, "btn btn-primary", waiting ? undefined : "play");
      run.disabled = waiting;
      acts.append(run);
    }
    if (t.kind !== "manual") acts.append(this.#button("toggle", t.enabled ? "Pause" : "Resume", t.id, "btn", t.enabled ? "pause" : "play"));
    if (!running) acts.append(this.#button("edit", "Edit", t.id, "btn", "pen"), this.#button("duplicate", "Duplicate", t.id, "btn", "copy"),
      this.#button("delete", this.#confirm === t.id ? "Click again to delete" : "Delete", t.id, "btn btn-quiet", "trash"));

    const sec = (title: string, ...kids: (HTMLElement | string)[]): HTMLElement => {
      const s = el("section", "tp-d-sec");
      s.append(el("h4", undefined, title));
      for (const k of kids) s.append(typeof k === "string" ? el("p", "tp-pre", k) : k);
      return s;
    };
    const chips = el("div", "tp-chips");
    for (const c of toolChips(t)) { const tag = el("span", "tp-tag", c); tag.title = `Tool: ${c}`; chips.append(tag); }

    const tg = acts.querySelector<HTMLElement>('[data-op="toggle"]');
    if (tg) tg.title = TIPS[t.enabled ? "toggle:on" : "toggle:off"] ?? "";
    d.append(head, acts, sec("Instructions", t.prompt));
    if (t.extra) d.append(sec("Priority rules", t.extra));
    d.append(sec("Tools", chips));

    const hist = el("ol", "tp-hist");
    for (const r of t.history ?? []) {
      const li = el("li", "tp-run");
      li.dataset["status"] = r.status;
      const line = el("div", "tp-run-line");
      line.append(el("span", "tp-dot"), el("strong", undefined, STATUS_TEXT[r.status] ?? r.status), el("span", "tp-when-ago", `${ago(r.at)}${r.ms ? ` · ${seconds(r.ms)}` : ""}`));
      if (r.chat) line.append(this.#button("session", "Open session", r.chat, "btn btn-quiet"));
      li.append(line);
      if (r.result) li.append(el("p", undefined, r.result));
      hist.append(li);
    }
    d.append(sec(`History${t.runs ? ` (${t.runs} run${t.runs === 1 ? "" : "s"})` : ""}`, (t.history ?? []).length ? hist : "No runs yet."));
  }
}

customElements.define("ph-tasks", PhTasks);
