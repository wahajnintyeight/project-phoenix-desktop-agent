// <ph-model-list>: choose a model and a reasoning effort for one provider. It is used in
// each provider's Settings card and, with `compact`, in the quick picker in the agent panel.
//
// Go sends the catalog and the saved choice (`data`); the user's choice goes back as
// `v model <provider> <model> <effort>`. Everything shown comes from a CLI or config.yaml,
// so it is placed with textContent only. Any valid model id can be typed, not only listed
// ones, because a model can be newer than the CLI's list.
import { send } from "./protocol";

export interface ModelInfo {
  id: string;
  name: string;
  description?: string;
  contextWindow?: number;
  efforts?: string[];
  defaultEffort?: string;
}

export interface ModelsData {
  id: string;
  name: string;
  models: ModelInfo[];
  model: string;
  effort: string;
  error?: string;
}

const EFFORT_LABEL: Record<string, string> = { xhigh: "Extra high", max: "Max", ultra: "Ultra" };

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

function btn(label: string, cls: string): HTMLButtonElement {
  const b = el("button", cls, label);
  b.type = "button";
  return b;
}

function cap(s: string): string {
  return EFFORT_LABEL[s] ?? s.charAt(0).toUpperCase() + s.slice(1);
}

function ctx(n: number): string {
  return n >= 1_000_000 ? `${+(n / 1_000_000).toFixed(1)}M context` : `${Math.round(n / 1000)}K context`;
}

/** "Haiku · Low": what is currently chosen, for a summary line or a button. */
export function describeChoice(d: ModelsData | null): string {
  if (!d) return "";
  const m = d.models.find((x) => x.id === d.model);
  const name = d.model === "" ? "CLI default" : (m?.name ?? d.model);
  return d.effort ? `${name} · ${cap(d.effort)}` : name;
}

export class PhModelList extends HTMLElement {
  #d: ModelsData | null = null;
  readonly #list = el("div", "ml-list");
  readonly #effort = el("div", "ml-effort");
  readonly #note = el("p", "muted small ml-note");
  readonly #custom = el("input", "ml-custom");
  readonly #useCustom = btn("Use", "btn");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#list.setAttribute("role", "radiogroup");
    this.#list.setAttribute("aria-label", "Model");

    this.#custom.type = "text";
    this.#custom.placeholder = "Other model ID";
    this.#custom.autocomplete = "off";
    this.#custom.spellcheck = false;
    this.#custom.maxLength = 100;
    this.#custom.setAttribute("aria-label", "Other model ID");
    this.#useCustom.dataset["act"] = "custom";
    const customRow = el("div", "row ml-custom-row");
    customRow.append(this.#custom, this.#useCustom);

    this.append(this.#list, this.#effort, customRow, this.#note);
    this.addEventListener("click", this.#onClick, { passive: true });
    this.#custom.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.#useCustomId();
    }, { passive: true });
    if (this.#d) this.#render();
  }

  set data(value: ModelsData) {
    this.#d = value;
    if (this.isConnected) this.#render();
  }

  get data(): ModelsData | null {
    return this.#d;
  }

  #selected(d: ModelsData): ModelInfo | undefined {
    return d.models.find((m) => m.id === d.model);
  }

  /** Efforts offered for the current model. A typed-in model gets the widest list known. */
  #efforts(d: ModelsData): string[] {
    const m = this.#selected(d);
    if (m) return m.efforts ?? [];
    return d.models.reduce<string[]>((best, x) => ((x.efforts?.length ?? 0) > best.length ? (x.efforts ?? []) : best), []);
  }

  #render(): void {
    const d = this.#d;
    if (!d) return;
    this.dataset["provider"] = d.id;

    const rows: HTMLElement[] = d.models.map((m) => {
      const b = btn("", "ml-row");
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(m.id === d.model));
      b.dataset["act"] = "model";
      b.dataset["model"] = m.id;
      const top = el("span", "ml-top");
      top.append(el("span", "ml-name", m.name));
      if (m.id !== "") top.append(el("span", "ml-id", m.id));
      if (m.contextWindow) top.append(el("span", "ml-meta", ctx(m.contextWindow)));
      b.append(top);
      if (m.description) b.append(el("span", "ml-desc", m.description));
      return b;
    });
    if (d.model !== "" && !this.#selected(d)) {
      const b = btn("", "ml-row");
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", "true");
      b.dataset["act"] = "model";
      b.dataset["model"] = d.model;
      const top = el("span", "ml-top");
      top.append(el("span", "ml-name", d.model), el("span", "ml-meta", "typed in"));
      b.append(top);
      rows.push(b);
    }
    this.#list.replaceChildren(...rows);

    const efforts = this.#efforts(d);
    const def = this.#selected(d)?.defaultEffort ?? "";
    if (efforts.length === 0) {
      this.#effort.replaceChildren();
      this.#effort.hidden = true;
    } else {
      this.#effort.hidden = false;
      const seg = el("div", "seg");
      seg.setAttribute("role", "radiogroup");
      seg.setAttribute("aria-label", "Reasoning effort");
      for (const e of ["", ...efforts]) {
        const b = btn(e === "" ? (def ? `Default (${cap(def)})` : "Default") : cap(e), "seg-btn");
        b.setAttribute("role", "radio");
        b.setAttribute("aria-checked", String(e === d.effort));
        b.dataset["act"] = "effort";
        b.dataset["effort"] = e;
        seg.append(b);
      }
      this.#effort.replaceChildren(el("span", "ml-label small muted", "Reasoning effort"), seg);
    }

    this.#note.textContent = d.error ?? (d.models.length === 0 ? "No models found. Install the CLI, then re-check." : "");
    this.#note.classList.toggle("is-error", Boolean(d.error));
  }

  #choose(model: string, effort: string): void {
    const d = this.#d;
    if (!d) return;
    this.#d = { ...d, model, effort };
    this.#render(); // optimistic; Go confirms or reports an error
    send("v", "model", d.id, model, effort);
  }

  #useCustomId(): void {
    const v = this.#custom.value.trim();
    if (!v || !/^[A-Za-z0-9][A-Za-z0-9._:/[\]-]{0,99}$/.test(v)) {
      this.#note.className = "muted small ml-note is-error";
      this.#note.textContent = "A model ID uses letters, digits and . _ : / [ ] -, and cannot start with a dash.";
      return;
    }
    this.#custom.value = "";
    this.#choose(v, "");
  }

  #onClick = (e: Event): void => {
    const t = (e.target as Element | null)?.closest<HTMLElement>("[data-act]");
    const d = this.#d;
    if (!t || !d) return;
    switch (t.dataset["act"]) {
      case "model": {
        const id = t.dataset["model"] ?? "";
        const next = d.models.find((m) => m.id === id);
        const keep = next ? (next.efforts ?? []).includes(d.effort) : true;
        this.#choose(id, keep ? d.effort : "");
        break;
      }
      case "effort":
        this.#choose(d.model, t.dataset["effort"] ?? "");
        break;
      case "custom":
        this.#useCustomId();
        break;
    }
  };
}

customElements.define("ph-model-list", PhModelList);
