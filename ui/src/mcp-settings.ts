// <ph-mcp>: install and manage MCP servers. Go owns the list, validates every definition and
// keeps secrets in the vault; this element sends what the user typed and draws what Go
// reports. Installing is two steps (stage, then confirm) so the user always sees the exact
// command that will run. Everything from a server (its name, tool names and descriptions) is
// untrusted text and is placed with textContent only.
import { el } from "./dom";
import { send } from "./protocol";

interface ServerView {
  name: string;
  transport: "stdio" | "http";
  summary: string;
  enabled: boolean;
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
  error?: string;
  /** Set once after the pending servers were installed: the form is cleared. */
  installed?: boolean;
}

interface TestResult {
  name: string;
  ok: boolean;
  error?: string;
  server?: string;
  version?: string;
  tools?: { name: string; description?: string }[];
}

type Mode = "json" | "command" | "url";

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
  readonly #list = el("div", "mcp-list");
  readonly #empty = el("p", "text-fg-muted", "No servers installed yet.");
  readonly #notice = el("p", "msg");
  readonly #review = el("div", "mcp-review");
  readonly #seg = el("div", "seg");
  readonly #form = el("div", "mcp-form");
  readonly #json = area('{ "mcpServers": { "name": { "command": "npx", "args": ["-y", "package"] } } }', "Server definition as JSON", 6);
  readonly #name = input("my-server", "Server name");
  readonly #command = input("npx", "Command");
  readonly #args = area("-y\n@scope/some-mcp-server", "Arguments, one per line");
  readonly #env = area("API_KEY=value", "Environment variables, one NAME=value per line");
  readonly #url = input("https://example.com/mcp", "Server address");
  readonly #headers = area("Authorization: Bearer value", "Headers, one Name: value per line");
  readonly #stage = el("button", "btn", "Review");
  #mode: Mode = "json";
  #tests = new Map<string, TestResult>();
  #busy = new Set<string>();
  #removing = "";
  #data: McpData = { servers: [] };

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#notice.setAttribute("aria-live", "polite");
    this.#review.hidden = true;
    this.#review.setAttribute("role", "group");
    this.#review.setAttribute("aria-label", "Review before installing");

    const add = el("details", "mcp-add");
    const sum = el("summary", undefined, "Add servers");
    sum.prepend(chevron());
    add.append(sum, this.#form);

    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "How to add a server");
    this.#seg.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-mode]");
      const m = b?.dataset["mode"] as Mode | undefined;
      if (m) {
        this.#mode = m;
        this.#renderForm();
      }
    }, { passive: true });
    this.#stage.type = "button";
    this.#stage.addEventListener("click", () => this.#submit(), { passive: true });

    const note = el("p", "max-w-[62ch] text-xs text-fg-muted", "A server can run programs and use your accounts with your Windows permissions. Only install ones you trust. Values for environment variables and headers are stored encrypted, never in the settings file.");
    this.append(this.#notice, this.#review, this.#list, this.#empty, add, note);

    this.#list.addEventListener("click", (e) => this.#onListClick(e), { passive: true });
    this.#list.addEventListener("change", (e) => {
      const c = e.target as HTMLInputElement | null;
      const name = c?.closest<HTMLElement>("[data-server]")?.dataset["server"];
      if (c?.matches("[data-enable]") && name) send("v", "mcp-enable", name, c.checked ? "1" : "0");
    }, { passive: true });
    this.#review.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-review]");
      if (!b) return;
      this.#notice.textContent = "";
      send("v", b.dataset["review"] === "confirm" ? "mcp-confirm" : "mcp-cancel");
    }, { passive: true });

    this.#renderForm();
    this.#render();
  }

  set data(value: McpData) {
    this.#data = value;
    if (value.installed) {
      for (const f of [this.#json, this.#name, this.#command, this.#args, this.#env, this.#url, this.#headers]) f.value = "";
      this.#say("Installed. Turn on “Agent can use” to let the agent call its tools.", false);
    }
    // A server that is gone loses its test result.
    for (const name of [...this.#tests.keys()]) {
      if (!value.servers.some((s) => s.name === name)) this.#tests.delete(name);
    }
    if (this.#removing && !value.servers.some((s) => s.name === this.#removing)) this.#removing = "";
    if (this.isConnected) this.#render();
  }

  set result(r: TestResult) {
    this.#busy.delete(r.name);
    this.#tests.set(r.name, r);
    if (this.isConnected) this.#render();
  }

  #onListClick(e: Event): void {
    const b = (e.target as Element | null)?.closest<HTMLElement>("[data-op]");
    const name = b?.closest<HTMLElement>("[data-server]")?.dataset["server"];
    if (!b || !name) return;
    switch (b.dataset["op"]) {
      case "test":
        this.#busy.add(name);
        this.#tests.delete(name);
        send("v", "mcp-test", name);
        this.#render();
        break;
      case "remove":
        // Two clicks: the first only asks.
        if (this.#removing === name) {
          this.#removing = "";
          send("v", "mcp-remove", name);
        } else {
          this.#removing = name;
          this.#render();
        }
        break;
      case "keep":
        this.#removing = "";
        this.#render();
        break;
    }
  }

  #submit(): void {
    this.#notice.textContent = "";
    let text = "";
    if (this.#mode === "json") {
      text = this.#json.value;
    } else {
      const name = this.#name.value.trim();
      if (!name) return this.#say("Give the server a name.", true);
      let def: Record<string, unknown>;
      if (this.#mode === "command") {
        def = { command: this.#command.value.trim(), args: lines(this.#args.value) };
        const env = pairs(this.#env.value, "=");
        if (Object.keys(env).length) def["env"] = env;
      } else {
        def = { url: this.#url.value.trim() };
        const headers = pairs(this.#headers.value, ":");
        if (Object.keys(headers).length) def["headers"] = headers;
      }
      text = JSON.stringify({ mcpServers: { [name]: def } });
    }
    send("v", "mcp-stage", text);
  }

  #say(text: string, error: boolean): void {
    this.#notice.className = `msg ${error ? "is-error" : "is-ok"}`;
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
    const parts: HTMLElement[] = [this.#seg];
    if (this.#mode === "json") {
      parts.push(field("Server definition", "The JSON from the server's install instructions. Several servers can be pasted at once.", this.#json));
    } else if (this.#mode === "command") {
      parts.push(
        field("Name", "Lowercase letters, digits and hyphens.", this.#name),
        field("Command", "The program to run, for example npx, node or python.", this.#command),
        field("Arguments", "One per line.", this.#args),
        field("Environment variables", "Optional. One NAME=value per line. Stored encrypted.", this.#env),
      );
    } else {
      parts.push(
        field("Name", "Lowercase letters, digits and hyphens.", this.#name),
        field("Address", "Starts with https:// (http only for this computer).", this.#url),
        field("Headers", "Optional, for example a token. One Name: value per line. Stored encrypted.", this.#headers),
      );
    }
    const row = el("div", "row");
    row.append(this.#stage);
    parts.push(row);
    this.#form.replaceChildren(...parts);
  }

  #render(): void {
    const { servers, pending, error } = this.#data;
    if (error) this.#say(error, true);
    else if (this.#notice.classList.contains("is-error")) this.#notice.textContent = "";

    this.#empty.hidden = servers.length > 0;
    this.#list.hidden = servers.length === 0;
    this.#list.replaceChildren(...servers.map((s) => this.#item(s)));

    const p = pending ?? [];
    this.#review.hidden = p.length === 0;
    if (p.length > 0) {
      const confirm = el("button", "btn btn-primary", p.length === 1 ? "Install" : `Install ${p.length} servers`);
      confirm.type = "button";
      confirm.dataset["review"] = "confirm";
      const cancel = el("button", "btn btn-quiet", "Cancel");
      cancel.type = "button";
      cancel.dataset["review"] = "cancel";
      const row = el("div", "row");
      row.append(confirm, cancel);
      this.#review.replaceChildren(
        el("h3", undefined, p.length === 1 ? "Install this server?" : `Install these ${p.length} servers?`),
        el("p", "text-fg-muted", "Installing does not start it. Testing it, or letting the agent use it, runs exactly this:"),
        ...p.flatMap((s) => {
          const out: HTMLElement[] = [el("p", "font-semibold", s.name), el("code", undefined, s.summary)];
          const secrets = [...(s.env ?? []), ...(s.headers ?? [])];
          if (secrets.length) out.push(el("p", "text-xs text-fg-muted", `Stored encrypted: ${secrets.join(", ")}`));
          return out;
        }),
        row,
      );
    }
  }

  #item(s: ServerView): HTMLElement {
    const item = el("div", "mcp-item");
    item.dataset["server"] = s.name;

    const id = el("div", "mcp-id");
    id.append(el("span", "mcp-name", s.name), el("span", "mcp-sub", s.summary));
    id.title = s.summary;

    const use = el("label", "mcp-use");
    const box = el("input");
    box.type = "checkbox";
    box.checked = s.enabled;
    box.dataset["enable"] = "";
    box.setAttribute("aria-label", `Let the agent use ${s.name}`);
    use.append(box, document.createTextNode("Agent can use"));

    const test = el("button", "btn btn-sm", this.#busy.has(s.name) ? "Connecting…" : "Test");
    test.type = "button";
    test.disabled = this.#busy.has(s.name);
    test.dataset["op"] = "test";

    const head = el("div", "mcp-head");
    head.append(id, use, test);
    if (this.#removing === s.name) {
      const yes = el("button", "btn btn-sm", "Remove");
      yes.type = "button";
      yes.dataset["op"] = "remove";
      const no = el("button", "btn btn-quiet btn-sm", "Keep");
      no.type = "button";
      no.dataset["op"] = "keep";
      head.append(yes, no);
    } else {
      const rm = el("button", "btn btn-quiet btn-sm", "Remove");
      rm.type = "button";
      rm.dataset["op"] = "remove";
      rm.setAttribute("aria-label", `Remove ${s.name}`);
      head.append(rm);
    }
    item.append(head);

    const t = this.#tests.get(s.name);
    if (t) item.append(this.#testBlock(t));
    return item;
  }

  #testBlock(t: TestResult): HTMLElement {
    if (!t.ok) return el("p", "msg is-error", t.error ?? "Could not connect.");
    const box = el("div", "mcp-tools");
    const who = [t.server, t.version].filter(Boolean).join(" ");
    const n = t.tools?.length ?? 0;
    box.append(el("p", "msg is-ok", `Connected${who ? ` to ${who}` : ""}. ${n === 0 ? "It offers no tools." : n === 1 ? "1 tool:" : `${n} tools:`}`));
    for (const tool of t.tools ?? []) {
      const row = el("div", "mcp-tool");
      row.append(el("b", undefined, tool.name), el("span", undefined, tool.description ?? ""));
      box.append(row);
    }
    return box;
  }
}

function chevron(): SVGElement {
  const ns = "http://www.w3.org/2000/svg";
  const svg = document.createElementNS(ns, "svg");
  svg.setAttribute("viewBox", "0 0 16 16");
  svg.setAttribute("width", "14");
  svg.setAttribute("height", "14");
  svg.setAttribute("aria-hidden", "true");
  const p = document.createElementNS(ns, "path");
  p.setAttribute("d", "m6 4 4 4-4 4");
  p.setAttribute("fill", "none");
  p.setAttribute("stroke", "currentColor");
  p.setAttribute("stroke-width", "1.5");
  p.setAttribute("stroke-linecap", "round");
  p.setAttribute("stroke-linejoin", "round");
  svg.append(p);
  return svg;
}

customElements.define("ph-mcp", PhMcp);
