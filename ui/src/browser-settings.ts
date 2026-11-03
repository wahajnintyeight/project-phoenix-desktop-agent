// <ph-browser-settings>: the address bar's search engine. Go validates and saves it
// (https only, one {query}); this element only offers presets and a custom address.
import { send } from "./protocol";

interface BrowserData {
  searchUrl: string;
}

interface Outcome {
  ok?: boolean;
  text?: string;
  error?: string;
}

const PRESETS: { name: string; url: string }[] = [
  { name: "DuckDuckGo", url: "https://duckduckgo.com/?q={query}" },
  { name: "Google", url: "https://www.google.com/search?q={query}" },
  { name: "Bing", url: "https://www.bing.com/search?q={query}" },
  { name: "Brave", url: "https://search.brave.com/search?q={query}" },
];

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

export class PhBrowserSettings extends HTMLElement {
  #url = "";
  #customOpen = false;
  readonly #seg = el("div", "seg");
  readonly #custom = el("input", "field");
  readonly #save = el("button", "btn", "Save");
  readonly #customRow = el("div", "row");
  readonly #result = el("p", "msg");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "Search engine");
    this.#custom.type = "text";
    this.#custom.placeholder = "https://example.com/search?q={query}";
    this.#custom.spellcheck = false;
    this.#custom.autocomplete = "off";
    this.#custom.setAttribute("aria-label", "Custom search address");
    this.#save.type = "button";
    this.#customRow.append(this.#custom, this.#save);
    this.#result.setAttribute("aria-live", "polite");

    const hint = el("p", "max-w-[62ch] text-xs text-fg-muted", "Used when you type something in the address bar that is not a web address. The address must start with https:// and contain {query} where the search words go.");
    this.append(this.#seg, this.#customRow, this.#result, hint);

    this.#seg.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-url],[data-custom]");
      if (!b) return;
      this.#result.textContent = "";
      if (b.dataset["custom"] !== undefined) {
        this.#customOpen = true;
        this.#render();
        this.#custom.focus();
      } else {
        this.#customOpen = false;
        send("v", "search", b.dataset["url"] ?? "");
      }
    }, { passive: true });
    this.#save.addEventListener("click", () => this.#submit(), { passive: true });
    this.#custom.addEventListener("keydown", (e) => {
      if (e.key === "Enter") this.#submit();
    }, { passive: true });
    this.#render();
  }

  set data(value: BrowserData) {
    this.#url = value.searchUrl;
    if (this.isConnected) this.#render();
  }

  set result(value: Outcome) {
    this.#result.className = `msg ${value.error ? "is-error" : "is-ok"}`;
    this.#result.textContent = value.error ?? value.text ?? "";
  }

  #submit(): void {
    const v = this.#custom.value.trim();
    if (v) send("v", "search", v);
  }

  #render(): void {
    const preset = PRESETS.find((p) => p.url === this.#url);
    const custom = this.#customOpen || (!preset && this.#url !== "");
    const buttons = PRESETS.map((p) => {
      const b = el("button", "seg-btn", p.name);
      b.type = "button";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(!custom && p.url === this.#url));
      b.dataset["url"] = p.url;
      return b;
    });
    const c = el("button", "seg-btn", "Custom");
    c.type = "button";
    c.setAttribute("role", "radio");
    c.setAttribute("aria-checked", String(custom));
    c.dataset["custom"] = "";
    this.#seg.replaceChildren(...buttons, c);

    this.#customRow.hidden = !custom;
    if (custom && document.activeElement !== this.#custom && !this.#custom.value) {
      this.#custom.value = preset ? "" : this.#url;
    }
  }
}

customElements.define("ph-browser-settings", PhBrowserSettings);
