// <ph-mcp>: install and manage MCP servers. Secret values stay in Go's encrypted vault;
// this component only receives their names and renders all server-supplied text safely.
import { el } from "./dom";
import { send } from "./protocol";

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
  /** Set after an installation or edit is saved; the form is cleared. */
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

type Mode = "json" | "command" | "url";
type View = "list" | "detail" | "edit";

function field(label: string, hint: string | undefined, control: HTMLElement): HTMLLabelElement {
  const l = el("label");
  l.append(document.createTextNode(label));
  if (hint) l.append(el("small", undefined, hint));
  l.append(control);
  return l;
}

function input(placeholder: string, label: string): HTMLInputElement {
  const i = el("input", "field");
  i.type = "text";
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

const lines = (s: string): string[] => s.split("\n").map((l) => l.replace(/\r$/, "")).filter((l) => l.trim() !== "");

function pairs(s: string, sep: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const l of lines(s)) {
    const i = l.indexOf(sep);
    if (i > 0) out[l.slice(0, i).trim()] = l.slice(i + sep.length).trim();
  }
  return out;
}

export class PhMcp extends HTMLElement {
  readonly #overview = el("div", "mcp-overview");
  readonly #detail = el("div", "mcp-detail");
  readonly #editor = el("div", "mcp-editor");
  readonly #editorSlot = el("div", "mcp-editor-slot");
  readonly #list = el("div", "mcp-list");
  readonly #summary = el("p", "mcp-summary");
  readonly #empty = el("div", "mcp-empty");
  readonly #notice = el("p", "msg mcp-notice");
  readonly #review = el("div", "mcp-review");
  readonly #seg = el("div", "seg");
  readonly #form = el("div", "mcp-form");
  readonly #add = el("details", "mcp-add");
  readonly #json = area('{ "mcpServers": { "name": { "command": "npx", "args": ["-y", "package"] } } }', "Server definition as JSON", 6);
  readonly #name = input("my-server", "Server name");
  readonly #command = input("npx", "Command");
  readonly #args = area("-y\n@scope/some-mcp-server", "Arguments, one per line");
  readonly #env = area("API_KEY=value", "Environment variables, one NAME=value per line");
  readonly #url = input("https://example.com/mcp", "Server address");
  readonly #headers = area("Authorization: Bearer value", "Headers, one Name: value per line");
  readonly #stage = el("button", "btn btn-primary", "Review installation");
  #mode: Mode = "json";
  #view: View = "list";
  #selected = "";
  #editing = "";
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
    this.#notice.setAttribute("role", "status");
    this.#notice.setAttribute("aria-live", "polite");
    this.#review.hidden = true;
    this.#review.setAttribute("role", "group");
    this.#review.setAttribute("aria-label", "Review before saving");

    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "How to add a server");
    this.#seg.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-mode]");
      const m = b?.dataset["mode"] as Mode | undefined;
      if (m && !this.#editing) {
        this.#mode = m;
        this.#renderForm();
      }
    }, { passive: true });
    this.#stage.type = "button";
    this.#stage.addEventListener("click", () => this.#submit(), { passive: true });

    const summary = el("summary", "btn btn-primary mcp-add-trigger");
    summary.prepend(plus());
    summary.append(document.createTextNode("Add servers"));
    this.#add.append(summary, this.#form);

    this.#empty.append(
      el("h3", undefined, "No MCP servers yet"),
      el("p", "text-fg-muted", "Add a trusted server to give the agent access to its tools. Test the connection first, then choose whether the agent can use it."),
    );

    this.#overview.append(this.#summary, this.#list, this.#empty, this.#add,
      el("p", "mcp-safety text-xs text-fg-muted", "Program servers run with your Windows permissions. Credentials are stored encrypted and never shown here."));
    const editorTitle = el("h3", "mcp-editor-title");
    editorTitle.tabIndex = -1;
    this.#editor.append(
      backButton("Server details", "back-detail"),
      editorTitle,
      el("p", "text-fg-muted mcp-editor-help", "Change the address or launch command. Review changes before saving."),
      this.#editorSlot,
    );

    this.append(this.#notice, this.#review, this.#overview, this.#detail, this.#editor);
    this.addEventListener("click", (e) => this.#onAction(e));
    this.addEventListener("change", (e) => this.#onChange(e));
    this.#review.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-review]");
      if (!b) return;
      this.#notice.textContent = "";
      send("v", b.dataset["review"] === "confirm" ? "mcp-confirm" : "mcp-cancel");
    });

    this.#renderForm();
    this.#render();
  }

  set data(value: McpData) {
    this.#data = value;
    if (value.installed) {
      const editing = this.#editing;
      this.#add.open = false;
      if (editing) {
        this.#editing = "";
        this.#view = "detail";
        this.#selected = editing;
        this.#tests.delete(editing);
        this.#add.append(this.#form);
        this.#say("Connection settings saved. Test the server to confirm it is ready.", false);
      } else {
        this.#say("Server added and left off. Open its details to test the connection and choose whether the agent can use it.", false);
      }
      this.#resetForm();
    }
    for (const name of [...this.#tests.keys()]) {
      if (!value.servers.some((s) => s.name === name)) this.#tests.delete(name);
    }
    for (const name of [...this.#busy]) {
      if (!value.servers.some((s) => s.name === name)) this.#busy.delete(name);
    }
    if (this.#selected && !value.servers.some((s) => s.name === this.#selected)) {
      this.#selected = "";
      this.#editing = "";
      this.#view = "list";
      this.#add.append(this.#form);
      this.#resetForm();
    }
    if (this.#removing && !value.servers.some((s) => s.name === this.#removing)) this.#removing = "";
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

  #onAction(e: Event): void {
    const b = (e.target as Element | null)?.closest<HTMLElement>("[data-op]");
    const name = b?.closest<HTMLElement>("[data-server]")?.dataset["server"] ?? "";
    if (!b) return;
    switch (b.dataset["op"]) {
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
      case "back-detail":
        this.#leaveEdit();
        break;
      case "edit": {
        const server = this.#data.servers.find((s) => s.name === name);
        if (server) this.#enterEdit(server);
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

  #enterEdit(server: ServerView): void {
    this.#editing = server.name;
    this.#selected = server.name;
    this.#view = "edit";
    this.#mode = server.transport === "http" ? "url" : "command";
    this.#name.value = server.name;
    this.#command.value = server.command ?? "";
    this.#args.value = (server.args ?? []).join("\n");
    this.#env.value = (server.env ?? []).map((key) => `${key}=`).join("\n");
    this.#url.value = server.url ?? "";
    this.#headers.value = (server.headers ?? []).map((key) => `${key}:`).join("\n");
    this.#add.open = false;
    this.#editorSlot.append(this.#form);
    const title = this.#editor.querySelector<HTMLElement>(".mcp-editor-title");
    if (title) title.textContent = `Edit ${server.name}`;
    this.#renderForm();
    this.#render();
    this.#focusHeading();
  }

  #leaveEdit(): void {
    this.#editing = "";
    this.#view = "detail";
    this.#add.append(this.#form);
    this.#resetForm();
    this.#render();
    this.#focusHeading();
  }

  #resetForm(): void {
    this.#mode = "json";
    for (const f of [this.#json, this.#name, this.#command, this.#args, this.#env, this.#url, this.#headers]) f.value = "";
    this.#renderForm();
  }

  #submit(): void {
    this.#notice.textContent = "";
    const name = this.#editing || this.#name.value.trim();
    if (!name) return this.#say("Give the server a name.", true);
    let def: Record<string, unknown>;
    if (this.#mode === "json" && !this.#editing) {
      send("v", "mcp-stage", this.#json.value);
      return;
    }
    if (this.#mode === "command") {
      def = { command: this.#command.value.trim(), args: lines(this.#args.value) };
      const env = pairs(this.#env.value, "=");
      if (Object.keys(env).length) def["env"] = env;
    } else {
      def = { url: this.#url.value.trim() };
      const headers = pairs(this.#headers.value, ":");
      if (Object.keys(headers).length) def["headers"] = headers;
    }
    const text = JSON.stringify({ mcpServers: { [name]: def } });
    if (this.#editing) send("v", "mcp-update-stage", this.#editing, text);
    else send("v", "mcp-stage", text);
  }

  #say(text: string, error: boolean): void {
    this.#notice.className = `msg mcp-notice ${error ? "is-error" : "is-ok"}`;
    this.#notice.setAttribute("role", error ? "alert" : "status");
    this.#notice.textContent = text;
  }

  #renderForm(): void {
    const modes: [Mode, string][] = [["json", "Paste JSON"], ["command", "Program"], ["url", "Web address"]];
    this.#seg.replaceChildren(...modes.map(([m, label]) => {
      const b = el("button", "seg-btn", label);
      b.type = "button";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(this.#mode === m));
      b.dataset["mode"] = m;
      return b;
    }));
    const parts: HTMLElement[] = [];
    if (!this.#editing) parts.push(this.#seg);
    if (this.#mode === "json" && !this.#editing) {
      parts.push(field("Server definition", "Paste the server's JSON. Several servers can be added at once.", this.#json));
    } else if (this.#mode === "command") {
      if (!this.#editing) parts.push(field("Name", "Lowercase letters, digits and hyphens.", this.#name));
      parts.push(
        field("Command", "The program to run, for example npx, node or python.", this.#command),
        field("Arguments", "One per line.", this.#args),
        field("Environment variables", this.#editing
          ? "Leave an existing NAME= blank to keep it. Remove its line to delete it; add NAME=value to replace or add a credential."
          : "Optional. One NAME=value per line. Values are stored encrypted.", this.#env),
      );
    } else {
      if (!this.#editing) parts.push(field("Name", "Lowercase letters, digits and hyphens.", this.#name));
      parts.push(
        field("Address", "Starts with https:// (http only for this computer).", this.#url),
        field("Headers", "One Name: value per line. Values are stored encrypted.", this.#headers),
      );
      if (this.#editing) parts.push(el("p", "mcp-form-hint text-xs text-fg-muted", "Leave an existing header blank to keep it. Remove its line to delete it; enter a value to replace it."));
    }
    this.#stage.textContent = this.#editing ? "Review changes" : "Review installation";
    const row = el("div", "row");
    row.append(this.#stage);
    parts.push(row);
    this.#form.replaceChildren(...parts);
  }

  #render(): void {
    const { servers, pending, error } = this.#data;
    if (error) this.#say(error, true);
    else if (this.#notice.classList.contains("is-error")) this.#say("", false);

    this.#overview.hidden = this.#view !== "list";
    this.#detail.hidden = this.#view !== "detail";
    this.#editor.hidden = this.#view !== "edit";
    this.#renderOverview(servers);
    if (this.#view === "detail") this.#renderDetail();
    const staged = pending ?? [];
    this.#review.hidden = staged.length === 0;
    if (staged.length) this.#renderReview(staged, Boolean(this.#data.updating));
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
    const back = backButton("All servers", "back-list");
    top.append(back, this.#status(server.name));

    const title = el("h3", "mcp-detail-title", server.name);
    title.tabIndex = -1;
    const transport = el("span", "chip", server.transport === "http" ? "Streamable HTTP" : "Local program");
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
    const edit = el("button", "btn btn-quiet", "Edit connection");
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
    this.#detail.querySelector<HTMLElement>(".mcp-detail-title")!.tabIndex = -1;
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
    const cancel = el("button", "btn btn-quiet", "Cancel");
    cancel.type = "button";
    cancel.dataset["review"] = "cancel";
    const row = el("div", "row");
    row.append(confirm, cancel);
    this.#review.replaceChildren(
      el("h3", undefined, updating ? "Review connection changes" : pending.length === 1 ? "Review server before installing" : `Review ${pending.length} servers before installing`),
      el("p", "text-fg-muted", updating
        ? "Saving does not test this connection. If agent access is on, Phoenix may start it on the next agent turn. Blank credential values keep their saved value; removed keys are deleted."
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
      const selector = this.#view === "edit" ? ".mcp-editor-title" : ".mcp-detail-title";
      this.querySelector<HTMLElement>(selector)?.focus();
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
    return { title: "The server did not accept the sign-in", advice: "Choose Test / sign in and complete authorization in your browser. If it still fails, check that the signed-in account has access." };
  }
  if (message.includes("forbidden") || /\b403\b/.test(message)) {
    return { title: "The account does not have access", advice: "Check that this account is allowed to use the server, then test again." };
  }
  if (message.includes("timeout") || message.includes("deadline exceeded")) {
    return { title: "The connection timed out", advice: "Check that the server is running and its address is reachable, then try again." };
  }
  return { title: "Phoenix could not complete the connection", advice: "Check the address or launch command and the server's authentication requirements, then test again." };
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
