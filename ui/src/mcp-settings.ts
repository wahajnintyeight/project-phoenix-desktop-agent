// <ph-mcp>: add, edit and manage MCP servers. Secret values stay in Go's encrypted vault; this
// component only receives their names and renders all server-supplied text safely.
//
// One screen at a time: the list, a server's details, the editor (the same form adds a server
// and edits one; its type and name can always be changed) and the review of what is about to be
// saved. Go stages and validates everything, so what the review shows is what gets installed.
import { el } from "./dom";
import { send } from "./protocol";
import { lines, normalizeName, splitCommand, suggestName, type Kind } from "./mcp-names";

interface ServerView {
  name: string;
  transport: "stdio" | "http";
  summary: string;
  command?: string;
  args?: string[];
  url?: string;
  enabled: boolean;
  authorized?: boolean;
  env?: string[];
  headers?: string[];
}

interface PendingView {
  name: string;
  transport: "stdio" | "http";
  summary: string;
  env?: string[];
  headers?: string[];
}

interface McpData {
  servers: ServerView[];
  pending?: PendingView[];
  updating?: boolean;
  error?: string;
  /** Set after an installation or edit is saved. */
  installed?: boolean;
}

interface TestResult {
  name: string;
  ok: boolean;
  canceled?: boolean;
  error?: string;
  server?: string;
  version?: string;
  tools?: { name: string; description?: string }[];
}

type View = "list" | "detail" | "editor";

// ── helpers ────────────────────────────────────────────────────────────────

function field(label: string, hint: string | undefined, control: HTMLElement): HTMLLabelElement {
  const l = el("label");
  l.append(document.createTextNode(label));
  if (hint) l.append(el("small", undefined, hint));
  l.append(control);
  return l;
}

function input(placeholder: string, label: string, type = "text"): HTMLInputElement {
  const i = el("input", "field");
  i.type = type;
  i.placeholder = placeholder;
  i.spellcheck = false;
  i.autocomplete = "off";
  i.setAttribute("aria-label", label);
  return i;
}

function area(placeholder: string, label: string, rows = 3): HTMLTextAreaElement {
  const t = el("textarea", "field");
  t.placeholder = placeholder;
  t.rows = rows;
  t.spellcheck = false;
  t.autocomplete = "off";
  t.setAttribute("aria-label", label);
  return t;
}

/** Name and value rows for headers or environment variables. A saved value is never shown: a
 *  blank value keeps it, a typed one replaces it, and removing the row deletes it. */
class Pairs {
  readonly box = el("div", "mcp-kv");
  readonly #rows = el("div", "mcp-kv-rows");
  readonly #add: HTMLButtonElement;
  readonly #nameHint: string;

  constructor(nameHint: string, addLabel: string) {
    this.#nameHint = nameHint;
    this.#add = el("button", "btn btn-quiet btn-sm", addLabel);
    this.#add.type = "button";
    this.#add.addEventListener("click", () => {
      const row = this.#row("", "", false);
      this.#rows.append(row);
      row.querySelector<HTMLInputElement>("input")?.focus();
    }, { passive: true });
    this.box.append(this.#rows, this.#add);
  }

  /** Replaces the rows. `saved` lists names whose values are already stored. */
  set(saved: string[], filled: Record<string, string> = {}): void {
    this.#rows.replaceChildren(...saved.map((k) => this.#row(k, filled[k] ?? "", true)));
  }

  clear(): void {
    this.#rows.replaceChildren();
  }

  /** Rows with a name; blank value means "keep the saved one" (Go ignores it for a new server). */
  values(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const row of Array.from(this.#rows.querySelectorAll<HTMLElement>(".mcp-kv-row"))) {
      const [k, v] = Array.from(row.querySelectorAll<HTMLInputElement>("input"));
      const key = k?.value.trim() ?? "";
      if (key) out[key] = v?.value ?? "";
    }
    return out;
  }

  #row(key: string, value: string, saved: boolean): HTMLElement {
    const row = el("div", "mcp-kv-row");
    const k = input(this.#nameHint, "Name");
    k.value = key;
    const v = input(saved ? "Saved. Leave blank to keep" : "Value", "Value", "password");
    v.value = value;
    const rm = el("button", "btn btn-quiet btn-sm", "Remove");
    rm.type = "button";
    rm.setAttribute("aria-label", "Remove this row");
    rm.addEventListener("click", () => row.remove(), { passive: true });
    row.append(k, v, rm);
    return row;
  }
}

// ── component ──────────────────────────────────────────────────────────────

export class PhMcp extends HTMLElement {
  readonly #notice = el("p", "msg mcp-notice");
  readonly #overview = el("div", "mcp-overview");
  readonly #detail = el("div", "mcp-detail");
  readonly #editor = el("div", "mcp-editor");
  readonly #review = el("div", "mcp-review");
  readonly #list = el("div", "mcp-list");
  readonly #summary = el("p", "mcp-summary");
  readonly #empty = el("div", "mcp-empty");
  readonly #seg = el("div", "seg");
  readonly #form = el("div", "mcp-form");
  readonly #formNotice = el("p", "msg mcp-notice");
  readonly #title = el("h3", "mcp-editor-title");
  readonly #json = area('{ "mcpServers": { "name": { "command": "npx", "args": ["-y", "package"] } } }', "Server definition as JSON", 8);
  readonly #name = input("", "Server name");
  readonly #command = input("npx -y @scope/some-mcp-server", "Command");
  readonly #args = area("One per line (optional)", "Arguments, one per line", 2);
  readonly #url = input("https://example.com/mcp", "Server address");
  readonly #headers = new Pairs("Header name, for example Authorization", "Add header");
  readonly #env = new Pairs("Variable name, for example API_KEY", "Add variable");
  readonly #submitBtn = el("button", "btn btn-primary", "Review");
  #kind: Kind = "url";
  #view: View = "list";
  #selected = "";
  #editing = ""; // the server being edited, "" when adding
  #submittedName = ""; // the name last sent for review, to open its details after saving
  #lastStaged: string[] = []; // the names on the review screen, for a pasted definition
  #tests = new Map<string, TestResult>();
  #busy = new Set<string>();
  #removing = "";
  #data: McpData = { servers: [] };

  get workingServers(): { name: string; toolCount: number }[] {
    const working: { name: string; toolCount: number }[] = [];
    for (const server of this.#data.servers) {
      const result = this.#tests.get(server.name);
      if (server.enabled && result?.ok) working.push({ name: server.name, toolCount: result.tools?.length ?? 0 });
    }
    return working;
  }

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    for (const n of [this.#notice, this.#formNotice]) {
      n.setAttribute("role", "status");
      n.setAttribute("aria-live", "polite");
    }
    this.#review.hidden = true;
    this.#review.setAttribute("role", "group");
    this.#review.setAttribute("aria-label", "Review before saving");

    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "Type of server");
    this.#seg.addEventListener("click", (e) => {
      const k = (e.target as Element | null)?.closest<HTMLElement>("[data-kind]")?.dataset["kind"] as Kind | undefined;
      if (k && k !== this.#kind) this.#setKind(k);
    }, { passive: true });
    for (const f of [this.#name, this.#url, this.#command]) {
      f.addEventListener("input", () => this.#syncSuggestion(), { passive: true });
      f.addEventListener("keydown", (e) => { if (e.key === "Enter") this.#submit(); });
    }
    this.#args.addEventListener("input", () => this.#syncSuggestion(), { passive: true });
    this.#submitBtn.type = "button";
    this.#submitBtn.addEventListener("click", () => this.#submit(), { passive: true });

    this.#empty.append(
      el("h3", undefined, "No MCP servers yet"),
      el("p", "text-fg-muted", "Add a trusted server to give the agent access to its tools. You review the connection before anything is saved, then test it and choose whether the agent can use it."),
    );

    const addButton = el("button", "btn btn-primary mcp-add-trigger");
    addButton.type = "button";
    addButton.dataset["op"] = "add";
    addButton.append(plus(), document.createTextNode("Add server"));
    this.#overview.append(this.#summary, this.#list, this.#empty, addButton,
      el("p", "mcp-safety text-xs text-fg-muted", "Program servers run with your Windows permissions. Credentials are stored encrypted and never shown here."));

    this.#title.tabIndex = -1;
    this.#editor.append(backButton("Back", "back-editor"), this.#title, this.#form);

    this.append(this.#notice, this.#review, this.#overview, this.#detail, this.#editor);
    this.addEventListener("click", (e) => this.#onAction(e));
    this.addEventListener("change", (e) => this.#onChange(e));
    this.#review.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-review]");
      if (!b) return;
      this.#say("", false);
      send("v", b.dataset["review"] === "confirm" ? "mcp-confirm" : "mcp-cancel");
    });

    this.#renderForm();
    this.#render();
  }

  set data(value: McpData) {
    const previous = this.#data;
    this.#data = value;
    if (value.installed) {
      const was = this.#editing;
      // A pasted definition has no name of its own here; one server means open that one.
      const saved = this.#submittedName || (this.#lastStaged.length === 1 ? this.#lastStaged[0] ?? "" : "");
      const isNew = (n: string) => !previous.servers.some((s) => s.name === n);
      if (was && saved && saved !== was) { // renamed: the old name's test and state no longer apply
        this.#tests.delete(was);
        this.#busy.delete(was);
      }
      this.#editing = "";
      this.#tests.delete(saved);
      this.#resetForm();
      if (saved && value.servers.some((s) => s.name === saved)) {
        this.#selected = saved;
        this.#view = "detail";
        this.#say(was
          ? "Saved. Test the server to confirm it is ready."
          : isNew(saved) ? "Added and left off. Test the connection, then turn on agent access." : "Saved.", false);
      } else {
        this.#view = "list";
        this.#say("Servers added and left off. Open one to test it and turn on agent access.", false);
      }
      this.#focusHeading();
    }
    for (const name of [...this.#tests.keys()]) {
      if (!value.servers.some((s) => s.name === name)) this.#tests.delete(name);
    }
    for (const name of [...this.#busy]) {
      if (!value.servers.some((s) => s.name === name)) this.#busy.delete(name);
    }
    if (this.#view === "detail" && !value.servers.some((s) => s.name === this.#selected)) {
      this.#selected = "";
      this.#view = "list";
    }
    if (this.#removing && !value.servers.some((s) => s.name === this.#removing)) this.#removing = "";
    // An error belongs to the data push that carried it: show it once, and clear it when the
    // next push has none, not on every redraw (so it cannot linger into the next form).
    if (value.error) this.#say(value.error, true);
    else if (!value.installed && (this.#notice.classList.contains("is-error") || this.#formNotice.classList.contains("is-error"))) this.#say("", false);
    if (this.isConnected) this.#render();
  }

  set result(r: TestResult) {
    this.#busy.delete(r.name);
    if (!this.#data.servers.some((s) => s.name === r.name) || r.canceled) {
      this.#tests.delete(r.name);
    } else {
      this.#tests.set(r.name, r);
    }
    if (this.isConnected) this.#render();
  }

  // ── actions ──────────────────────────────────────────────────────────────

  #onAction(e: Event): void {
    const b = (e.target as Element | null)?.closest<HTMLElement>("[data-op]");
    const name = b?.closest<HTMLElement>("[data-server]")?.dataset["server"] ?? "";
    if (!b) return;
    switch (b.dataset["op"]) {
      case "add":
        this.#enterEditor(null);
        break;
      case "open":
        if (!name) return;
        this.#selected = name;
        this.#view = "detail";
        this.#render();
        this.#focusHeading();
        break;
      case "back-list":
        this.#view = "list";
        this.#selected = "";
        this.#render();
        break;
      case "back-editor": // leave the editor without saving
        this.#editing = "";
        this.#say("", false);
        this.#view = this.#selected && this.#data.servers.some((s) => s.name === this.#selected) ? "detail" : "list";
        this.#render();
        this.#focusHeading();
        break;
      case "edit": {
        const server = this.#data.servers.find((s) => s.name === name);
        if (server) this.#enterEditor(server);
        break;
      }
      case "test":
        if (!name) return;
        if (this.#busy.has(name)) {
          send("v", "mcp-test", name); // the same operation cancels an active test
        } else {
          this.#busy.add(name);
          this.#tests.delete(name);
          send("v", "mcp-test", name);
        }
        this.#render();
        this.#focusAction("test", name);
        break;
      case "remove":
        this.#removing = name;
        this.#render();
        this.#focusAction("keep", name);
        break;
      case "remove-confirm":
        this.#removing = "";
        send("v", "mcp-remove", name);
        break;
      case "keep":
        this.#removing = "";
        this.#render();
        this.#focusAction("remove", name);
        break;
    }
  }

  #onChange(e: Event): void {
    const c = e.target as HTMLInputElement | null;
    const name = c?.closest<HTMLElement>("[data-server]")?.dataset["server"];
    if (c?.matches("[data-enable]") && name) send("v", "mcp-enable", name, c.checked ? "1" : "0");
  }

  /** Opens the editor to add a server (null) or to edit one. */
  #enterEditor(server: ServerView | null): void {
    this.#resetForm();
    this.#editing = server?.name ?? "";
    if (server) {
      this.#selected = server.name;
      this.#kind = server.transport === "http" ? "url" : "command";
      this.#name.value = server.name;
      this.#command.value = server.command ?? "";
      this.#args.value = (server.args ?? []).join("\n");
      this.#url.value = server.url ?? "";
      this.#headers.set(server.headers ?? []);
      this.#env.set(server.env ?? []);
    }
    this.#view = "editor";
    this.#say("", false);
    this.#renderForm();
    this.#render();
    this.#focusHeading();
  }

  #setKind(kind: Kind): void {
    this.#kind = kind;
    this.#say("", false);
    this.#renderForm();
  }

  #resetForm(): void {
    this.#kind = "url";
    for (const f of [this.#json, this.#name, this.#command, this.#args, this.#url]) f.value = "";
    this.#headers.clear();
    this.#env.clear();
    this.#renderForm();
  }

  #suggestion(): string {
    return suggestName(this.#kind, this.#url.value, [this.#command.value, ...lines(this.#args.value)].join(" "));
  }

  #syncSuggestion(): void {
    this.#name.placeholder = this.#suggestion() || "my-server";
  }

  #submit(): void {
    this.#say("", false);
    if (this.#kind === "json") {
      if (!this.#json.value.trim()) return this.#say("Paste the server's JSON first.", true);
      this.#submittedName = "";
      send("v", "mcp-stage", this.#json.value);
      return;
    }
    const name = normalizeName(this.#name.value) || this.#suggestion();
    if (!name) return this.#say("Enter a name, or fill in the address or command and Phoenix will suggest one.", true);
    let def: Record<string, unknown>;
    if (this.#kind === "url") {
      const url = this.#url.value.trim();
      if (!url) return this.#say("Enter the server's web address.", true);
      def = { url };
      const headers = this.#headers.values();
      if (Object.keys(headers).length) def["headers"] = headers;
    } else {
      const words = splitCommand(this.#command.value);
      const extra = lines(this.#args.value);
      if (words.length === 0) return this.#say("Enter the program to run, for example npx.", true);
      // "npx -y pkg" typed into Command works: the first word is the program, the rest arguments.
      def = { command: words[0], args: [...words.slice(1), ...extra] };
      const env = this.#env.values();
      if (Object.keys(env).length) def["env"] = env;
    }
    const text = JSON.stringify({ mcpServers: { [name]: def } });
    this.#submittedName = name;
    if (this.#editing) send("v", "mcp-update-stage", this.#editing, text);
    else send("v", "mcp-stage", text);
  }

  #say(text: string, error: boolean): void {
    for (const n of [this.#notice, this.#formNotice]) {
      n.className = `msg mcp-notice ${error ? "is-error" : "is-ok"}`;
      n.setAttribute("role", error ? "alert" : "status");
      n.textContent = "";
    }
    (this.#view === "editor" ? this.#formNotice : this.#notice).textContent = text;
  }

  // ── rendering ────────────────────────────────────────────────────────────

  #renderForm(): void {
    const kinds: [Kind, string][] = [["url", "Web address"], ["command", "Program"]];
    if (!this.#editing) kinds.push(["json", "Paste JSON"]);
    this.#seg.replaceChildren(...kinds.map(([k, label]) => {
      const b = el("button", "seg-btn", label);
      b.type = "button";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(this.#kind === k));
      b.dataset["kind"] = k;
      return b;
    }));
    const parts: HTMLElement[] = [this.#seg];
    if (this.#kind === "json") {
      parts.push(field("Server definition", "Paste the JSON the server's install instructions give. Several servers can be added at once.", this.#json));
    } else {
      parts.push(field("Name", "Lowercase letters, digits and hyphens. Leave blank to use the suggestion.", this.#name));
      if (this.#kind === "url") {
        parts.push(
          field("Web address", "The server's Streamable HTTP address. It starts with https:// (http only for this computer).", this.#url),
          field("Headers", this.#editing
            ? "Credentials sent with every request. Saved values are hidden: leave a value blank to keep it, type to replace it, or remove the row to delete it."
            : "Optional, for example Authorization with a Bearer token. Values are stored encrypted.", this.#headers.box),
        );
      } else {
        parts.push(
          field("Command", "The program and, if you like, its arguments: for example npx -y @scope/some-mcp-server.", this.#command),
          field("More arguments", "Optional, one per line.", this.#args),
          field("Environment variables", this.#editing
            ? "Saved values are hidden: leave a value blank to keep it, type to replace it, or remove the row to delete it."
            : "Optional, for example an API key. Values are stored encrypted.", this.#env.box),
        );
      }
    }
    this.#submitBtn.textContent = this.#kind === "json" ? "Review installation" : this.#editing ? "Review changes" : "Review installation";
    const row = el("div", "row");
    row.append(this.#submitBtn);
    parts.push(this.#formNotice, row);
    this.#title.textContent = this.#editing ? `Edit ${this.#editing}` : "Add a server";
    this.#form.replaceChildren(...parts);
    this.#syncSuggestion();
  }

  #render(): void {
    const { servers, pending } = this.#data;
    const staged = pending ?? [];
    if (staged.length) this.#lastStaged = staged.map((s) => s.name);
    const reviewing = staged.length > 0;
    this.#review.hidden = !reviewing;
    this.#overview.hidden = reviewing || this.#view !== "list";
    this.#detail.hidden = reviewing || this.#view !== "detail";
    this.#editor.hidden = reviewing || this.#view !== "editor";
    this.#renderOverview(servers);
    if (this.#view === "detail") this.#renderDetail();
    if (reviewing) {
      this.#renderReview(staged, Boolean(this.#data.updating));
      this.#review.scrollIntoView({ block: "nearest" });
    }
  }

  #renderOverview(servers: ServerView[]): void {
    const enabled = servers.filter((s) => s.enabled).length;
    this.#summary.textContent = `${servers.length} ${servers.length === 1 ? "server" : "servers"} · ${enabled} enabled`;
    this.#empty.hidden = servers.length > 0;
    this.#list.hidden = servers.length === 0;
    this.#list.replaceChildren(...servers.map((s) => this.#item(s)));
  }

  #item(server: ServerView): HTMLElement {
    const item = el("div", "mcp-item");
    item.dataset["server"] = server.name;
    const main = el("div", "mcp-item-main");
    const nameLine = el("div", "mcp-item-title");
    const open = el("button", "mcp-open", server.name);
    open.type = "button";
    open.dataset["op"] = "open";
    open.setAttribute("aria-label", `Manage ${server.name}`);
    nameLine.append(open, this.#status(server.name));
    const endpoint = el("span", "mcp-sub", server.summary);
    endpoint.title = server.summary;
    main.append(nameLine, endpoint);

    const actions = el("div", "mcp-item-actions");
    actions.append(this.#accessToggle(server));
    const manage = el("button", "btn btn-sm", "Manage");
    manage.type = "button";
    manage.dataset["op"] = "open";
    actions.append(manage);
    item.append(main, actions);
    return item;
  }

  #renderDetail(): void {
    const server = this.#data.servers.find((s) => s.name === this.#selected);
    if (!server) return;
    this.#detail.dataset["server"] = server.name;
    const top = el("div", "mcp-detail-top");
    top.append(backButton("All servers", "back-list"), this.#status(server.name));

    const title = el("h3", "mcp-detail-title", server.name);
    title.tabIndex = -1;
    const transport = el("span", "chip", server.transport === "http" ? "Web address" : "Program");
    const heading = el("div", "mcp-detail-heading");
    heading.append(title, transport);
    const endpoint = el("code", "mcp-detail-endpoint", server.summary);
    endpoint.title = server.summary;

    const controls = el("div", "mcp-detail-actions");
    controls.append(this.#accessToggle(server));
    const testLabel = this.#busy.has(server.name) ? "Cancel test"
      : server.transport === "http" && !server.authorized ? "Test / sign in"
      : this.#tests.has(server.name) ? "Test again" : "Test connection";
    const test = el("button", "btn", testLabel);
    test.type = "button";
    test.dataset["op"] = "test";
    const edit = el("button", "btn btn-quiet", "Edit");
    edit.type = "button";
    edit.dataset["op"] = "edit";
    edit.disabled = this.#busy.has(server.name);
    controls.append(test, edit);

    const intro = el("p", "text-fg-muted mcp-detail-intro", server.enabled
      ? "This server is available automatically to supported providers."
      : "This server is installed but off. Test it, then turn on agent access when you are ready.");
    const authHint = server.transport === "http"
      ? el("p", "text-fg-muted mcp-detail-intro", "If the server requires sign-in, Phoenix opens its authorization page in your browser.")
      : null;

    const connection = el("section", "mcp-section");
    connection.append(el("h4", undefined, "Connection"), endpoint);

    const credentials = el("section", "mcp-section");
    credentials.append(el("h4", undefined, "Saved credentials"));
    const credentialNames = [
      ...(server.env ?? []).map((name) => `Environment · ${name}`),
      ...(server.headers ?? []).map((name) => `Header · ${name}`),
      ...(server.authorized ? ["Web sign-in · saved"] : []),
    ];
    credentials.append(credentialNames.length
      ? el("p", "text-fg-muted", `${credentialNames.join(", ")} · values are encrypted and never shown`)
      : el("p", "text-fg-muted", "No credentials are saved for this server."));

    const testSection = el("section", "mcp-section");
    testSection.append(el("h4", undefined, "Connection test"), this.#testBlock(server.name));

    const destructive = el("section", "mcp-section mcp-remove-section");
    destructive.append(el("h4", undefined, "Remove server"));
    if (this.#removing === server.name) {
      destructive.append(el("p", "text-fg-muted", "This removes the server and deletes its saved credentials. This cannot be undone."));
      const actions = el("div", "row");
      const remove = el("button", "btn btn-danger", "Remove server");
      remove.type = "button";
      remove.dataset["op"] = "remove-confirm";
      const keep = el("button", "btn btn-quiet", "Keep server");
      keep.type = "button";
      keep.dataset["op"] = "keep";
      actions.append(remove, keep);
      destructive.append(actions);
    } else {
      const remove = el("button", "btn btn-quiet", "Remove server");
      remove.type = "button";
      remove.dataset["op"] = "remove";
      destructive.append(remove);
    }

    this.#detail.replaceChildren(top, heading, controls, intro, ...(authHint ? [authHint] : []), connection, credentials, testSection, destructive);
  }

  #status(name: string): HTMLElement {
    const pill = el("span", "mcp-status");
    const dot = el("span", "dot");
    let text = "Not tested";
    let tone = "";
    if (this.#busy.has(name)) {
      text = "Testing";
      tone = "busy";
    } else {
      const result = this.#tests.get(name);
      if (result?.ok) {
        text = "Connected";
        tone = "ok";
      } else if (result) {
        text = "Needs attention";
        tone = "error";
      }
    }
    if (tone) dot.dataset["tone"] = tone;
    pill.append(dot, document.createTextNode(text));
    return pill;
  }

  #accessToggle(server: ServerView): HTMLLabelElement {
    const use = el("label", "mcp-use");
    const box = el("input");
    box.type = "checkbox";
    box.checked = server.enabled;
    box.dataset["enable"] = "";
    box.setAttribute("aria-label", `Let the agent use ${server.name}`);
    use.append(box, document.createTextNode("Agent access"));
    return use;
  }

  #testBlock(name: string): HTMLElement {
    if (this.#busy.has(name)) {
      const pending = el("div", "mcp-test-pending", "Connecting and checking tools. If required, finish sign-in in the browser that opens.");
      pending.setAttribute("role", "status");
      return pending;
    }
    const result = this.#tests.get(name);
    if (!result) return el("p", "text-fg-muted", "No test yet. Test the connection to check authentication and discover available tools.");
    if (!result.ok) {
      const { title, advice } = failureHelp(result.error ?? "");
      const box = el("div", "mcp-test-failure");
      box.setAttribute("role", "alert");
      box.append(el("p", "mcp-failure-title", title), el("p", "text-fg-muted", advice));
      const details = el("details", "mcp-error-details");
      details.append(el("summary", undefined, "Technical details"), el("code", undefined, result.error ?? "Could not connect."));
      box.append(details);
      return box;
    }
    const who = [result.server, result.version].filter(Boolean).join(" ");
    const tools = result.tools ?? [];
    const box = el("div", "mcp-test-success");
    box.setAttribute("role", "status");
    box.append(el("p", "mcp-connected", `Connected${who ? ` · ${who}` : ""}`));
    if (tools.length === 0) {
      box.append(el("p", "text-fg-muted", "The server connected but did not report any tools."));
      return box;
    }
    box.append(el("p", "text-fg-muted", `${tools.length} ${tools.length === 1 ? "tool" : "tools"} available`));
    const list = el("div", "mcp-tools");
    list.setAttribute("role", "list");
    for (const tool of tools) {
      const row = el("div", "mcp-tool");
      row.setAttribute("role", "listitem");
      row.append(el("b", undefined, tool.name), el("span", undefined, tool.description ?? "No description provided."));
      list.append(row);
    }
    box.append(list);
    return box;
  }

  #renderReview(pending: PendingView[], updating: boolean): void {
    const confirm = el("button", "btn btn-primary", updating ? "Save changes" : pending.length === 1 ? "Install server" : `Install ${pending.length} servers`);
    confirm.type = "button";
    confirm.dataset["review"] = "confirm";
    const cancel = el("button", "btn btn-quiet", "Back");
    cancel.type = "button";
    cancel.dataset["review"] = "cancel";
    const row = el("div", "row");
    row.append(confirm, cancel);
    this.#review.replaceChildren(
      el("h3", undefined, updating ? "Review changes" : pending.length === 1 ? "Review server before installing" : `Review ${pending.length} servers before installing`),
      el("p", "text-fg-muted", updating
        ? "Saving does not test the connection. If agent access is on, Phoenix may start it on the next agent turn. Blank credential values keep their saved value; removed rows are deleted."
        : "Installing leaves servers off. Testing or enabling one starts the exact connection shown below."),
      ...pending.flatMap((server) => {
        const out: HTMLElement[] = [el("p", "font-semibold mcp-review-name", server.name), el("code", "mcp-review-command", server.summary)];
        const secrets = [...(server.env ?? []), ...(server.headers ?? [])];
        if (secrets.length) out.push(el("p", "text-xs text-fg-muted", `Credential names: ${secrets.join(", ")}. Values stay encrypted.`));
        return out;
      }),
      row,
    );
  }

  #focusHeading(): void {
    requestAnimationFrame(() => {
      const selector = this.#view === "editor" ? ".mcp-editor-title" : this.#view === "detail" ? ".mcp-detail-title" : null;
      if (selector) this.querySelector<HTMLElement>(selector)?.focus();
    });
  }

  #focusAction(op: string, name: string): void {
    requestAnimationFrame(() => {
      const match = Array.from(this.querySelectorAll<HTMLElement>("[data-op]")).find((node) =>
        node.dataset["op"] === op && node.closest<HTMLElement>("[data-server]")?.dataset["server"] === name);
      match?.focus();
    });
  }
}

function failureHelp(error: string): { title: string; advice: string } {
  const message = error.toLowerCase();
  if (message.includes("unauthorized") || /\b401\b/.test(message)) {
    return { title: "The server did not accept the sign-in", advice: "Choose Test / sign in and complete authorization in your browser. If it still fails, check the token or header you saved, and that the account has access." };
  }
  if (message.includes("forbidden") || /\b403\b/.test(message)) {
    return { title: "The account does not have access", advice: "Check that this account is allowed to use the server, then test again." };
  }
  if (message.includes("timeout") || message.includes("deadline exceeded")) {
    return { title: "The connection timed out", advice: "Check that the server is running and its address is reachable, then try again." };
  }
  return { title: "Phoenix could not complete the connection", advice: "Check the address or launch command and the server's authentication requirements, then test again. You can change them with Edit." };
}

function plus(): SVGElement {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("width", "14");
  svg.setAttribute("height", "14");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "M8 3v10M3 8h10");
  path.setAttribute("fill", "none");
  path.setAttribute("stroke", "currentColor");
  path.setAttribute("stroke-width", "1.5");
  path.setAttribute("stroke-linecap", "round");
  svg.append(path);
  return svg;
}

function backButton(label: string, operation: string): HTMLButtonElement {
  const button = el("button", "btn btn-quiet mcp-back") as HTMLButtonElement;
  button.type = "button";
  button.dataset["op"] = operation;
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("width", "14");
  svg.setAttribute("height", "14");
  svg.setAttribute("aria-hidden", "true");
  const path = document.createElementNS(ns, "path");
  path.setAttribute("d", "m10 3-5 5 5 5M5 8h8");
  path.setAttribute("fill", "none");
  path.setAttribute("stroke", "currentColor");
  path.setAttribute("stroke-width", "1.5");
  path.setAttribute("stroke-linecap", "round");
  path.setAttribute("stroke-linejoin", "round");
  svg.append(path);
  button.append(svg, document.createTextNode(label));
  return button;
}

customElements.define("ph-mcp", PhMcp);
