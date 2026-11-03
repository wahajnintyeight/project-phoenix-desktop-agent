// <ph-provider>: one AI provider in Settings. Go sends it a status object (`data`) and the
// outcome of a test (`result`); it sends user intent back as "v" messages. All text from Go
// is placed with textContent: version strings and error messages come from external programs.
import { send } from "./protocol";
import { describeChoice, type ModelsData, type PhModelList } from "./model-list";

export interface ProviderData {
  id: string;
  name: string;
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

export class PhProvider extends HTMLElement {
  #data: ProviderData | null = null;
  readonly #name = el("h2");
  readonly #badge = el("span", "prov-badge");
  readonly #meta = el("p", "muted small prov-meta");
  readonly #modeCli = button("CLI sign-in", "mode-cli", "seg-btn");
  readonly #modeKey = button("API key", "mode-api_key", "seg-btn");
  readonly #cliLine = el("p", "prov-line");
  readonly #signIn = button("Sign in", "signin");
  readonly #keyLine = el("p", "prov-line");
  readonly #keyInput = el("input", "prov-key-input");
  readonly #saveKey = button("Save key", "savekey");
  readonly #clearKey = button("Remove key", "clearkey");
  readonly #makeDefault = button("Use as default", "default", "btn btn-quiet");
  readonly #test = button("Test", "test");
  readonly #refresh = button("Re-check", "refresh", "btn btn-quiet");
  readonly #result = el("p", "prov-result small");
  readonly #models = document.createElement("details");
  readonly #modelSummary = document.createElement("summary");
  readonly #modelList = document.createElement("ph-model-list") as PhModelList;

  static observedAttributes = ["data-state"];

  attributeChangedCallback(): void {
    if (this.isConnected) this.#applyState();
  }

  // Transient states set by Go: checking, testing, login. They override the resting labels.
  #applyState(): void {
    const s = this.dataset["state"] ?? "idle";
    this.#test.textContent = s === "testing" ? "Stop" : "Test";
    this.#test.disabled = s === "testing" ? false : !(this.#data?.installed && this.#data.ready) || s !== "idle";
    this.#signIn.disabled = s !== "idle";
    this.#refresh.disabled = s === "checking";
    if (s === "checking") this.#badge.textContent = "Checking…";
    else if (s === "login") this.#badge.textContent = "Waiting for sign-in…";
    else if (s === "testing") this.#badge.textContent = "Testing…";
    else if (this.#data) this.#badge.textContent = this.#badgeText(this.#data);
  }

  #badgeText(d: ProviderData): string {
    return !d.installed ? "Not installed" : d.ready ? "Ready" : d.mode === "api_key" ? "Needs key" : "Needs sign-in";
  }

  connectedCallback(): void {
    if (this.childElementCount > 0) return;

    this.#keyInput.type = "password";
    this.#keyInput.placeholder = "Paste API key";
    this.#keyInput.autocomplete = "off";
    this.#keyInput.spellcheck = false;
    this.#keyInput.setAttribute("aria-label", "API key");

    const head = el("div", "prov-head");
    head.append(this.#name, this.#badge, this.#makeDefault);

    const seg = el("div", "seg");
    seg.setAttribute("role", "radiogroup");
    seg.setAttribute("aria-label", "How Phoenix signs in");
    this.#modeCli.setAttribute("role", "radio");
    this.#modeKey.setAttribute("role", "radio");
    seg.append(this.#modeCli, this.#modeKey);

    const cli = el("div", "prov-cli");
    cli.append(this.#cliLine, this.#signIn);

    const key = el("div", "prov-keybox");
    const keyRow = el("div", "row");
    keyRow.append(this.#keyInput, this.#saveKey, this.#clearKey);
    key.append(this.#keyLine, keyRow);

    const actions = el("div", "row");
    actions.append(this.#test, this.#refresh);

    this.#models.className = "prov-models";
    this.#modelSummary.textContent = "Model";
    this.#models.append(this.#modelSummary, this.#modelList);

    this.append(head, this.#meta, seg, cli, key, this.#models, actions, this.#result);
    this.#result.setAttribute("aria-live", "polite");

    this.addEventListener("click", this.#onClick, { passive: true });
    this.#keyInput.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.#submitKey();
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
    this.#modelList.data = value;
    this.#modelSummary.textContent = `Model: ${describeChoice(value)}`;
  }

  set result(value: TestResult) {
    const r = this.#result;
    r.className = "prov-result small";
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
    r.textContent = parts.join(" ");
  }

  #id(): string {
    return this.dataset["provider"] ?? "";
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
  };

  #submitKey(): void {
    const v = this.#keyInput.value.trim();
    if (!v) return;
    send("v", "key", this.#id(), v);
    this.#keyInput.value = ""; // the key lives only in Go and the vault from here on
    this.#result.className = "prov-result small";
    this.#result.textContent = "";
  }

  #render(): void {
    const d = this.#data;
    if (!d) return;
    this.dataset["mode"] = d.mode;
    this.dataset["ready"] = String(d.ready);
    this.dataset["installed"] = String(d.installed);

    this.#name.textContent = d.name;
    this.#badge.dataset["tone"] = d.ready ? "ok" : "warn";
    this.#makeDefault.textContent = d.isDefault ? "Default provider" : "Use as default";
    this.#makeDefault.disabled = d.isDefault;

    const bits = [d.cliVersion ? `CLI ${d.cliVersion}` : "", d.cliPath ?? ""].filter(Boolean);
    this.#meta.textContent = bits.join(" · ") || d.problem || "";

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
      this.#keyLine.textContent = `API key ${where}. It is passed to the CLI for each run and never shown.`;
      this.#keyLine.classList.toggle("is-warn", d.keySource === "literal");
    } else {
      this.#keyLine.textContent = "No API key saved. It is encrypted for this Windows user and stored in the vault.";
      this.#keyLine.classList.remove("is-warn");
    }
    this.#clearKey.hidden = !(d.hasApiKey && d.keySource === "vault");
    this.#applyState();
  }
}

customElements.define("ph-provider", PhProvider);
