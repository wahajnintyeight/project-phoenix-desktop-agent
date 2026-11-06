// <ph-tasks>: saved automations. Go owns the data and runs the tasks; this element lists them,
// edits them and sends Run / Stop. Text is only ever placed with .value or textContent.
import { el } from "./dom";
import { send } from "./protocol";

interface Task {
  id: string;
  name: string;
  prompt: string;
  kind: "manual" | "interval" | "daily" | "weekdays";
  every?: number;
  at?: string | undefined;
  enabled: boolean;
  schedule: string;
  next?: number;
  lastRun?: number;
  lastStatus?: "ok" | "blocked" | "failed" | "stopped";
  lastResult?: string;
  lastMs?: number;
  runs?: number;
}

interface Data {
  tasks: Task[];
  running?: string | undefined;
  waiting?: string | undefined;
}

interface Outcome {
  ok?: boolean;
  text?: string;
  error?: string;
}

/** Starting points: each fills the editor, and the user adjusts the wording and the schedule. */
const STARTERS: { name: string; prompt: string; kind: Task["kind"]; at?: string | undefined }[] = [
  { name: "Morning job sweep", kind: "weekdays", at: "09:00",
    prompt: "Open my saved job searches and job alerts, find new listings that match my profile since the last run, and apply to the best matches. Skip any I already applied to." },
  { name: "Inbox check", kind: "daily", at: "08:30",
    prompt: "Open my email in the browser, read what arrived since yesterday, and report in one line how many need a reply and from whom." },
  { name: "Watch a page", kind: "manual",
    prompt: "Open the page I name here, read it, and report what changed since the last run: " },
];

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
  const same = d.toDateString() === today.toDateString();
  const tomorrow = new Date(today.getTime() + 86400000).toDateString() === d.toDateString();
  const time = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" }).format(d);
  if (same) return time;
  if (tomorrow) return `tomorrow ${time}`;
  return `${new Intl.DateTimeFormat(undefined, { weekday: "short" }).format(d)} ${time}`;
}

const STATUS_TEXT: Record<string, string> = { ok: "Done", blocked: "Blocked", failed: "Failed", stopped: "Stopped" };

class PhTasks extends HTMLElement {
  #data: Data = { tasks: [] };
  #editing: Task | null = null; // the task in the editor; id "" for a new one
  #confirmDelete = "";
  readonly #list = el("div", "tk-list");
  readonly #form = el("form", "tk-form");
  readonly #name = el("input", "field field-wide");
  readonly #prompt = el("textarea", "tk-text");
  readonly #kind = el("select", "field");
  readonly #every = el("input", "field");
  readonly #unit = el("select", "field");
  readonly #at = el("input", "field");
  readonly #err = el("p", "msg");
  readonly #newBtn = el("button", "btn btn-primary btn-sm", "New task");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    const head = el("div", "activity-head");
    this.#newBtn.type = "button";
    head.append(el("h2", undefined, "Tasks"), this.#newBtn);

    this.#name.placeholder = "Name";
    this.#name.setAttribute("aria-label", "Task name");
    this.#name.maxLength = 60;
    this.#prompt.placeholder = "What should the agent do? Say where to go, what to do and what counts as finished. It will not ask questions while it runs.";
    this.#prompt.setAttribute("aria-label", "Instructions");
    this.#prompt.rows = 6;
    for (const [v, l] of [["manual", "Only when I press Run"], ["interval", "Repeat every…"], ["daily", "Every day at…"], ["weekdays", "Weekdays at…"]] as const) {
      const o = el("option", undefined, l);
      o.value = v;
      this.#kind.append(o);
    }
    this.#kind.setAttribute("aria-label", "When");
    this.#every.type = "number";
    this.#every.min = "1";
    this.#every.value = "1";
    this.#every.setAttribute("aria-label", "Interval");
    for (const [v, l] of [["60", "hours"], ["1", "minutes"]] as const) {
      const o = el("option", undefined, l);
      o.value = v;
      this.#unit.append(o);
    }
    this.#unit.setAttribute("aria-label", "Interval unit");
    this.#at.type = "time";
    this.#at.value = "09:00";
    this.#at.setAttribute("aria-label", "Time of day");
    this.#err.setAttribute("aria-live", "polite");
    const save = el("button", "btn btn-primary btn-sm", "Save");
    save.type = "submit";
    const cancel = el("button", "btn btn-quiet btn-sm", "Cancel");
    cancel.type = "button";
    const when_ = el("div", "tk-when");
    when_.append(this.#kind, this.#every, this.#unit, this.#at);
    const actions = el("div", "tk-actions");
    actions.append(save, cancel);
    this.#form.append(this.#name, this.#prompt, when_, this.#err, actions);
    this.#form.hidden = true;

    this.append(head, this.#list, this.#form);

    this.#kind.addEventListener("change", () => this.#syncWhen(), { passive: true });
    this.#newBtn.addEventListener("click", () => this.#open({ id: "", name: "", prompt: "", kind: "manual", enabled: false, schedule: "" }), { passive: true });
    cancel.addEventListener("click", () => this.#close(), { passive: true });
    this.#form.addEventListener("submit", (e) => { e.preventDefault(); this.#save(); });
    this.#list.addEventListener("click", (e) => this.#click(e), { passive: true });
    this.#render();
  }

  set data(d: Data) {
    this.#data = { tasks: Array.isArray(d?.tasks) ? d.tasks : [], running: d?.running, waiting: d?.waiting };
    this.#render();
  }

  set result(r: Outcome) {
    if (!this.#editing) return;
    if (r.error) { this.#err.textContent = r.error; return; }
    this.#close();
  }

  #syncWhen(): void {
    const k = this.#kind.value;
    this.#every.hidden = this.#unit.hidden = k !== "interval";
    this.#at.hidden = k !== "daily" && k !== "weekdays";
  }

  #open(t: Task): void {
    this.#editing = t;
    this.#name.value = t.name;
    this.#prompt.value = t.prompt;
    this.#kind.value = t.kind;
    const mins = t.every ?? 60;
    const hours = mins % 60 === 0;
    this.#unit.value = hours ? "60" : "1";
    this.#every.value = String(hours ? mins / 60 : mins);
    this.#at.value = t.at || "09:00";
    this.#err.textContent = "";
    this.#syncWhen();
    this.#form.hidden = false;
    this.#list.hidden = true;
    this.#newBtn.hidden = true;
    (t.prompt ? this.#prompt : this.#name).focus();
  }

  #close(): void {
    this.#editing = null;
    this.#form.hidden = true;
    this.#list.hidden = false;
    this.#newBtn.hidden = false;
    this.#render();
  }

  #save(): void {
    const t = this.#editing;
    if (!t) return;
    if (!this.#prompt.value.trim()) { this.#err.textContent = "Say what the task should do."; return; }
    const kind = this.#kind.value;
    const every = kind === "interval" ? String(Math.round(Number(this.#every.value) * Number(this.#unit.value))) : "0";
    this.#err.textContent = "";
    send("k", "save", t.id, this.#name.value, this.#prompt.value, kind, every, kind === "daily" || kind === "weekdays" ? this.#at.value : "");
  }

  #click(e: Event): void {
    const b = (e.target as Element | null)?.closest<HTMLElement>("[data-op]");
    if (!b) return;
    const id = b.dataset["id"] ?? "";
    const task = this.#data.tasks.find((t) => t.id === id);
    switch (b.dataset["op"]) {
      case "run":
        send("k", "run", id);
        document.querySelector<HTMLElement>('[data-pane-btn="chat"]')?.click(); // watch it work
        break;
      case "stop": send("k", "stop"); break;
      case "watch": document.querySelector<HTMLElement>('[data-pane-btn="chat"]')?.click(); break;
      case "toggle": if (task) send("k", "enable", id, task.enabled ? "0" : "1"); break;
      case "edit": if (task) this.#open(task); break;
      case "delete":
        if (this.#confirmDelete !== id) {
          this.#confirmDelete = id;
          this.#render();
          setTimeout(() => { if (this.#confirmDelete === id) { this.#confirmDelete = ""; this.#render(); } }, 4000);
        } else {
          this.#confirmDelete = "";
          send("k", "delete", id);
        }
        break;
      case "starter": {
        const s = STARTERS[Number(id)];
        if (s) this.#open({ id: "", name: s.name, prompt: s.prompt, kind: s.kind, at: s.at, enabled: false, schedule: "" });
        break;
      }
    }
  }

  #render(): void {
    if (!this.isConnected && this.childElementCount === 0) return;
    const { tasks, running, waiting } = this.#data;
    this.#list.replaceChildren();
    if (tasks.length === 0) {
      const empty = el("div", "tk-empty");
      empty.append(
        el("h3", undefined, "Hand off work that repeats"),
        el("p", undefined, "A task is an instruction the agent carries out on its own, on a schedule or when you press Run. It decides, acts and reports in one line."),
      );
      const row = el("div", "tk-starters");
      STARTERS.forEach((s, i) => {
        const b = el("button", "btn btn-sm", s.name);
        b.type = "button";
        b.dataset["op"] = "starter";
        b.dataset["id"] = String(i);
        row.append(b);
      });
      empty.append(row, el("p", "tk-hint", "You can also tell the chat: \"every weekday at 9 do this\"."));
      this.#list.append(empty);
      return;
    }
    for (const t of tasks) this.#list.append(this.#card(t, running === t.id, waiting === t.id));
  }

  #card(t: Task, running: boolean, waiting: boolean): HTMLElement {
    const card = el("article", "tk-card");
    card.dataset["state"] = running ? "running" : waiting ? "waiting" : (t.lastStatus ?? "new");
    const top = el("div", "tk-top");
    top.append(el("span", "tk-dot"), el("h3", "tk-name", t.name));
    const sched = el("p", "tk-sched");
    sched.textContent = t.kind === "manual"
      ? "Manual"
      : `${t.schedule}${t.enabled ? (t.next ? ` · next ${when(t.next)}` : "") : " · off"}`;
    card.append(top, sched);

    const line = el("p", "tk-result");
    if (running) line.textContent = "Running now…";
    else if (waiting) line.textContent = "Waiting for the agent to be free…";
    else if (t.lastRun) line.textContent = `${STATUS_TEXT[t.lastStatus ?? "ok"] ?? ""} ${ago(t.lastRun)}${t.lastResult ? ": " + t.lastResult : ""}`;
    else line.textContent = "Not run yet.";
    card.append(line);

    const acts = el("div", "tk-actions");
    const btn = (op: string, label: string, cls = "btn btn-quiet btn-sm"): HTMLButtonElement => {
      const b = el("button", cls, label);
      b.type = "button";
      b.dataset["op"] = op;
      b.dataset["id"] = t.id;
      return b;
    };
    if (running) acts.append(btn("stop", "Stop", "btn btn-sm"), btn("watch", "Watch"));
    else acts.append(btn("run", waiting ? "Waiting" : "Run", "btn btn-primary btn-sm"));
    if (t.kind !== "manual" && !running) acts.append(btn("toggle", t.enabled ? "Turn off" : "Turn on"));
    if (!running) acts.append(btn("edit", "Edit"), btn("delete", this.#confirmDelete === t.id ? "Click again to delete" : "Delete"));
    if (waiting) (acts.firstElementChild as HTMLButtonElement).disabled = true;
    card.append(acts);
    return card;
  }
}

customElements.define("ph-tasks", PhTasks);
