// <ph-profile-settings>: the user's notes about themselves. Go stores them, cleans them and works
// out how much reaches the agent; this element is an editor with an honest meter, so the user can
// see what each message costs. The text is only ever placed with .value or textContent.
import { send } from "./protocol";

interface ProfileData {
  text: string;
  total: number;
  max: number;
  chars: number;
  tokens: number;
  budget: number;
  omitted: string[];
  template: string;
}

interface Outcome {
  ok?: boolean;
  text?: string;
  error?: string;
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

export class PhProfileSettings extends HTMLElement {
  #saved = "";
  #template = "";
  #max = 8000;
  #budget = 2000;
  readonly #area = el("textarea", "field profile-area");
  readonly #meter = el("div", "profile-meter");
  readonly #bar = el("div", "profile-bar");
  readonly #fill = el("div", "profile-fill");
  readonly #count = el("p", "text-xs text-fg-muted");
  readonly #sent = el("p", "profile-sent");
  readonly #left = el("p", "text-xs text-fg-muted");
  readonly #save = el("button", "btn btn-primary", "Save");
  readonly #template$ = el("button", "btn", "Start from a template");
  readonly #result = el("p", "msg");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#area.rows = 14;
    this.#area.spellcheck = true;
    this.#area.maxLength = this.#max;
    this.#area.placeholder = "Write in your own words, in markdown. Put what matters most first.\n\nFor example: who you are and what you do, how direct you like answers, what you like, what to never do, and what you are working toward.";
    this.#area.setAttribute("aria-label", "About you");
    this.#save.type = "button";
    this.#template$.type = "button";
    this.#result.setAttribute("aria-live", "polite");
    this.#bar.append(this.#fill);
    this.#meter.append(this.#bar, this.#count);

    const actions = el("div", "profile-actions");
    actions.append(this.#save, this.#template$);
    const hint = el("p", "max-w-[62ch] text-xs text-fg-muted", "Stays on this computer, in Phoenix's own database. A short version of this goes with every message so the agent can fit its help to you. If it is long, Phoenix sends whole sections from the top and tells the agent what was left out; the agent can read the rest when it matters. Do not put passwords or secrets here.");
    this.append(this.#area, this.#meter, this.#sent, this.#left, actions, this.#result, hint);

    this.#area.addEventListener("input", () => { this.#result.textContent = ""; this.#update(); }, { passive: true });
    this.#save.addEventListener("click", () => send("v", "profile-save", this.#area.value), { passive: true });
    this.#template$.addEventListener("click", () => {
      this.#area.value = this.#template;
      this.#update();
      this.#area.focus();
    });
    this.#update();
  }

  set data(v: ProfileData) {
    this.#saved = v.text;
    this.#template = v.template;
    this.#max = v.max;
    this.#budget = v.budget;
    this.#area.maxLength = v.max;
    if (document.activeElement !== this.#area || this.#area.value === "") this.#area.value = v.text;
    this.#update(v);
  }

  set result(v: Outcome) {
    this.#result.className = `msg ${v.error ? "is-error" : "is-ok"}`;
    this.#result.textContent = v.error ?? v.text ?? "";
  }

  #update(server?: ProfileData): void {
    const text = this.#area.value;
    const chars = [...text].length;
    const dirty = text !== this.#saved;
    this.#save.disabled = !dirty;
    this.#template$.hidden = text.trim() !== "";
    this.#count.textContent = `${chars.toLocaleString()} / ${this.#max.toLocaleString()} characters`;
    const sentChars = Math.min(chars, this.#budget);
    this.#fill.style.width = `${Math.min(100, (chars / this.#max) * 100)}%`;
    this.#bar.dataset["over"] = String(chars > this.#budget);

    if (!text.trim()) {
      this.#sent.textContent = "Nothing is sent to the agent yet.";
      this.#left.textContent = "";
      return;
    }
    // Exact figures come from Go after a save; while typing, an estimate (about 4 characters a token).
    const tokens = server && !dirty ? server.tokens : Math.ceil(sentChars / 4) + 45;
    this.#sent.textContent = dirty ? `Sent with every message: up to about ${tokens.toLocaleString()} tokens. Save for the exact figure.` : `Sent with every message: about ${tokens.toLocaleString()} tokens.`;
    if (chars > this.#budget) {
      const omitted = server && !dirty ? server.omitted : [];
      this.#left.textContent = omitted.length
        ? `Over the per-message budget, so these sections stay out until the agent asks for them: ${omitted.join(", ")}.`
        : `Over the per-message budget of ${this.#budget.toLocaleString()} characters: the start is sent, the rest is read by the agent only when needed. Put what matters most first.`;
    } else {
      this.#left.textContent = "All of it fits in every message.";
    }
  }
}

customElements.define("ph-profile-settings", PhProfileSettings);
