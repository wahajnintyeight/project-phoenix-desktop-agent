// <ph-provider>: one AI provider in Settings, shown as a row that opens into its details.
// Go sends it a status object (`data`), its model list (`models`) and the outcome of a test
// (`result`); it sends user intent back as "v" messages. All text from Go is placed with
// textContent: version strings, model names and error messages come from external programs
// and servers.
import { send } from "./protocol";
import { describeChoice, type ModelsData, type PhModelList } from "./model-list";

export interface ProviderData {
  id: string;
  name: string;
  kind: "cli" | "api";
  installed: boolean;
  cliPath?: string;
  cliVersion?: string;
  login: { signedIn: boolean; method?: string; plan?: string; account?: string };
  mode: "cli" | "api_key";
  hasApiKey: boolean;
  keySource?: string; // "vault" | "env" | "literal"
  ready: boolean;
  problem?: string;
  isDefault: boolean;
  baseUrl?: string;
  defaultBaseUrl?: string;
  keyHelp?: string;
  baseHint?: string;
}

interface TestResult {
  ok?: boolean;
  text?: string;
  error?: string;
  ms?: number;
  cost?: number;
  model?: string; // the concrete model that answered
  limits?: { window: string; utilization: number }[];
}

const METHOD: Record<string, string> = {
  "claude.ai": "Claude subscription",
  console: "Anthropic Console",
  chatgpt: "ChatGPT plan",
  "api-key": "an API key",
};

const SIGN_IN_LABEL: Record<string, string> = { claude: "Sign in with Claude", codex: "Sign in with ChatGPT" };

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function button(label: string, act: string, cls = "btn"): HTMLButtonElement {
  const b = el("button", cls, label);
  b.type = "button";
  b.dataset["act"] = act;
  return b;
}

function field(placeholder: string, label: string, mono = false): HTMLInputElement {
  const i = el("input", mono ? "field field-mono" : "field");
  i.autocomplete = "off";
  i.spellcheck = false;
  i.placeholder = placeholder;
  i.setAttribute("aria-label", label);
  return i;
}

function section(label: string, ...children: HTMLElement[]): HTMLElement {
  const s = el("div", "pv-section");
  s.append(el("h4", "pv-label", label), ...children);
  return s;
}

export class PhProvider extends HTMLElement {
  #data: ProviderData | null = null;
  #choice: ModelsData | null = null;

  readonly #root = el("details", "pv");
  readonly #name = el("span", "pv-name-text");
  readonly #defaultTag = el("span", "pv-default", "Default");
  readonly #sub = el("span", "pv-sub");
  readonly #status = el("span", "pv-status");
  readonly #modelText = el("span", "pv-model");

  readonly #modeCli = button("CLI sign-in", "mode-cli", "seg-btn");
  readonly #modeKey = button("API key", "mode-api_key", "seg-btn");
  readonly #modeSection: HTMLElement;
  readonly #cliLine = el("p", "msg");
  readonly #signIn = button("Sign in", "signin");
  readonly #cliSection: HTMLElement;

  readonly #keyLine = el("p", "msg");
  readonly #keyInput = field("Paste API key", "API key");
  readonly #saveKey = button("Save key", "savekey", "btn btn-primary");
  readonly #clearKey = button("Remove key", "clearkey", "btn btn-quiet");
  readonly #keyHelp = el("p", "msg");
  readonly #keySection: HTMLElement;

  readonly #baseInput = field("https://", "Base URL", true);
  readonly #baseHint = el("p", "msg");
  readonly #endpointSection: HTMLElement;

  readonly #modelList = document.createElement("ph-model-list") as PhModelList;
  readonly #makeDefault = button("Use as default", "default", "btn btn-quiet push");
  readonly #test = button("Test", "test");
  readonly #refresh = button("Re-check", "refresh", "btn btn-quiet");
  readonly #result = el("p", "msg");
  readonly #meta = el("p", "msg small");

  static observedAttributes = ["data-state"];

  constructor() {
    super();
    const seg = el("div", "seg");
    seg.setAttribute("role", "radiogroup");
    seg.setAttribute("aria-label", "How Phoenix signs in");
    this.#modeCli.setAttribute("role", "radio");
    this.#modeKey.setAttribute("role", "radio");
    seg.append(this.#modeCli, this.#modeKey);
    this.#modeSection = section("Sign-in method", seg);

    this.#cliSection = section("Sign-in", this.#cliLine, this.#signIn);

    this.#keyInput.type = "password";
    const keyRow = el("div", "row");
    keyRow.append(this.#keyInput, this.#saveKey, this.#clearKey);
    this.#keySection = section("API key", this.#keyLine, keyRow, this.#keyHelp);

    const endpoint = el("details", "pv-endpoint");
    const baseRow = el("div", "row");
    baseRow.append(this.#baseInput, button("Save address", "savebase"), button("Use default", "resetbase", "btn btn-quiet"));
    const endpointBody = el("div", "pv-endpoint-body");
    endpointBody.append(baseRow, this.#baseHint);
    endpoint.append(el("summary", undefined, "Change the endpoint"), endpointBody);
    this.#endpointSection = el("div", "pv-section");
    this.#endpointSection.append(endpoint);
  }

  attributeChangedCallback(): void {
    if (this.isConnected) this.#applyState();
  }

  connectedCallback(): void {
    if (this.childElementCount > 0) return;

    const summary = el("summary");
    const id = el("span", "pv-id");
    const nameRow = el("span", "pv-name");
    nameRow.append(this.#name, this.#defaultTag);
    id.append(nameRow, this.#sub);
    summary.append(id, this.#status, this.#modelText, el("span", "pv-chevron"));
    this.#root.name = "providers"; // opening one row closes the others

    const body = el("div", "pv-body");
    body.append(
      this.#meta,
      this.#modeSection,
      this.#cliSection,
      this.#keySection,
      this.#endpointSection,
      section("Model", this.#modelList),
    );
    const actions = el("div", "pv-actions");
    actions.append(this.#test, this.#refresh, this.#makeDefault);
    body.append(actions, this.#result);
    this.#result.setAttribute("aria-live", "polite");

    this.#root.append(summary, body);
    this.append(this.#root);

    this.addEventListener("click", this.#onClick, { passive: true });
    this.#keyInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.#submitKey();
    }, { passive: true });
    this.#baseInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.#saveBase();
    }, { passive: true });
    if (this.#data) this.#render();
  }

  get data(): ProviderData | null {
    return this.#data;
  }

  set data(value: ProviderData) {
    this.#data = value;
    if (this.isConnected) this.#render();
  }

  set models(value: ModelsData) {
    this.#choice = value;
    this.#modelList.data = value;
    this.#refreshModelText();
  }

  // The row's model column. A provider that cannot run yet and has no model chosen shows
  // nothing, so a list of unconfigured providers is not a column of "No model chosen".
  #refreshModelText(): void {
    const v = this.#choice;
    const idle = this.#data?.ready === false && v?.model === "";
    this.#modelText.textContent = v && !idle ? describeChoice(v) : "";
  }

  set result(value: TestResult) {
    const r = this.#result;
    r.className = "msg";
    if (value.error) {
      r.classList.add("is-error");
      r.textContent = value.error;
      return;
    }
    r.classList.add("is-ok");
    const parts = [`Works. It replied “${value.text ?? ""}” in ${((value.ms ?? 0) / 1000).toFixed(1)} s.`];
    if (value.model) parts.push(`Model used: ${value.model}.`);
    const limits = (value.limits ?? []).map((l) => `${l.window === "five_hour" ? "5-hour" : "7-day"} limit ${Math.round(l.utilization * 100)}% used`);
    if (limits.length) parts.push(limits.join(", ") + ".");
    if (value.cost) parts.push(`Cost about $${value.cost.toFixed(4)}.`);
    r.textContent = parts.join(" ");
  }

  #id(): string {
    return this.dataset["provider"] ?? "";
  }

  // Transient states set by Go: checking, testing, login. They override the resting labels.
  #applyState(): void {
    const s = this.dataset["state"] ?? "idle";
    const d = this.#data;
    this.#test.textContent = s === "testing" ? "Stop" : "Test";
    this.#test.disabled = s === "testing" ? false : !(d?.installed && d.ready) || s !== "idle";
    this.#signIn.disabled = s !== "idle";
    this.#refresh.disabled = s === "checking";
    if (s === "checking") this.#setStatus("Checking…", "");
    else if (s === "login") this.#setStatus("Waiting for sign-in…", "");
    else if (s === "testing") this.#setStatus("Testing…", "");
    else if (d) this.#setStatus(this.#statusText(d), d.ready ? "ok" : "warn");
  }

  #setStatus(text: string, tone: string): void {
    this.#status.textContent = text;
    this.#status.dataset["tone"] = tone;
  }

  #statusText(d: ProviderData): string {
    if (!d.installed) return "Not installed";
    if (d.ready) return "Ready";
    return d.mode === "api_key" ? "Needs key" : "Needs sign-in";
  }

  #subText(d: ProviderData): string {
    if (!d.installed) return "Command-line client not found";
    if (d.kind === "api") return d.hasApiKey ? "API key saved" : "No API key";
    if (d.mode === "api_key") return d.hasApiKey ? "Using an API key" : "API key mode, no key saved";
    const l = d.login;
    if (!l.signedIn) return "Not signed in";
    return `${METHOD[l.method ?? ""] ?? "Signed in"}${l.plan ? `, ${l.plan} plan` : ""}`;
  }

  #onClick = (e: Event): void => {
    const t = (e.target as Element | null)?.closest<HTMLElement>("[data-act]");
    if (!t || (t as HTMLButtonElement).disabled) return;
    const id = this.#id();
    const act = t.dataset["act"] ?? "";
    if (act.startsWith("mode-")) send("v", "mode", id, act.slice(5));
    else if (act === "signin") send("v", "login", id);
    else if (act === "test") send("v", "test", id);
    else if (act === "savekey") this.#submitKey();
    else if (act === "clearkey") send("v", "clearkey", id);
    else if (act === "default") send("v", "default", id);
    else if (act === "refresh") send("v", "refresh");
    else if (act === "savebase") this.#saveBase();
    else if (act === "resetbase") send("v", "baseurl", id, "");
  };

  #submitKey(): void {
    const v = this.#keyInput.value.trim();
    if (!v) return;
    send("v", "key", this.#id(), v);
    this.#keyInput.value = ""; // the key lives only in Go and the vault from here on
    this.#result.className = "msg";
    this.#result.textContent = "";
  }

  #saveBase(): void {
    const v = this.#baseInput.value.trim();
    if (v) send("v", "baseurl", this.#id(), v);
  }

  #render(): void {
    const d = this.#data;
    if (!d) return;
    const api = d.kind === "api";
    this.dataset["kind"] = d.kind;
    this.dataset["mode"] = d.mode;
    this.dataset["ready"] = String(d.ready);

    this.#name.textContent = d.name;
    this.#defaultTag.hidden = !d.isDefault;
    this.#sub.textContent = this.#subText(d);
    this.#makeDefault.hidden = d.isDefault;
    this.#refreshModelText();

    const bits = [d.cliVersion ? `CLI ${d.cliVersion}` : "", d.cliPath ?? ""].filter(Boolean);
    this.#meta.textContent = api ? "" : bits.join(" · ") || d.problem || "";
    this.#meta.hidden = this.#meta.textContent === "";

    // Claude and Codex can use their own sign-in or a key; the others only a key.
    const keyMode = api || d.mode === "api_key";
    this.#modeSection.hidden = api;
    this.#cliSection.hidden = keyMode;
    this.#keySection.hidden = !keyMode;
    this.#endpointSection.hidden = !api;
    this.#modeCli.setAttribute("aria-checked", String(d.mode === "cli"));
    this.#modeKey.setAttribute("aria-checked", String(d.mode === "api_key"));

    const l = d.login;
    if (!d.installed) {
      this.#cliLine.textContent = d.problem ?? "";
    } else if (l.signedIn) {
      const how = METHOD[l.method ?? ""] ?? "the CLI";
      this.#cliLine.textContent = `Signed in with ${how}${l.plan ? `, ${l.plan} plan` : ""}${l.account ? ` (${l.account})` : ""}. Phoenix runs the CLI, which uses this sign-in.`;
    } else {
      this.#cliLine.textContent = "Not signed in.";
    }
    this.#signIn.textContent = l.signedIn ? "Sign in again" : (SIGN_IN_LABEL[d.id] ?? "Sign in");
    this.#signIn.hidden = !d.installed;

    if (d.hasApiKey) {
      const where = d.keySource === "vault" ? "stored in Phoenix's encrypted vault" : d.keySource === "env" ? "read from an environment variable" : "written in config.yaml as plain text. Save it again to move it to the vault";
      this.#keyLine.textContent = `API key ${where}. It is sent only to this provider and never shown.`;
      this.#keyLine.className = d.keySource === "literal" ? "msg is-warn" : "msg";
      this.#keyHelp.textContent = "";
    } else {
      this.#keyLine.textContent = "No API key saved.";
      this.#keyLine.className = "msg";
      this.#keyHelp.textContent = d.keyHelp ?? "";
    }
    this.#clearKey.hidden = !(d.hasApiKey && d.keySource === "vault");

    if (api) {
      if (document.activeElement !== this.#baseInput) this.#baseInput.value = d.baseUrl ?? "";
      const custom = (d.baseUrl ?? "") !== (d.defaultBaseUrl ?? "");
      this.#baseHint.textContent = [custom ? `Default: ${d.defaultBaseUrl ?? ""}` : "Using the default address.", d.baseHint ?? ""].filter(Boolean).join(" ");
    }
    this.#applyState();
  }
}

customElements.define("ph-provider", PhProvider);
