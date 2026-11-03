// <ph-quick-model>: the model picker in the agent panel's footer. One button shows the
// current provider and model; it opens a popover to switch provider, model and effort
// without going to Settings. It shows the same data as the Settings cards.
import { send } from "./protocol";
import { describeChoice, type ModelsData, PhModelList } from "./model-list";

interface QuickData {
  default: string;
  providers: ModelsData[];
}

function el<K extends keyof HTMLElementTagNameMap>(tag: K, cls?: string, text?: string): HTMLElementTagNameMap[K] {
  const e = document.createElement(tag);
  if (cls) e.className = cls;
  if (text !== undefined) e.textContent = text;
  return e;
}

export class PhQuickModel extends HTMLElement {
  #d: QuickData | null = null;
  readonly #button = el("button", "qm-btn");
  readonly #pop = el("div", "qm-pop");
  readonly #seg = el("div", "seg");
  readonly #list = document.createElement("ph-model-list") as PhModelList;

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#pop.id = "quick-model-pop";
    this.#pop.setAttribute("popover", "");
    this.#button.type = "button";
    this.#button.setAttribute("popovertarget", this.#pop.id);
    this.#button.setAttribute("aria-label", "Choose model");
    this.#button.disabled = true;
    this.#button.textContent = "Model";

    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "Provider");
    this.#seg.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-provider]");
      const id = b?.dataset["provider"];
      if (!id || !this.#d || id === this.#d.default) return;
      this.#d = { ...this.#d, default: id }; // optimistic; Go saves it and re-sends
      this.#render();
      send("v", "default", id);
    }, { passive: true });

    this.#list.classList.add("compact");
    this.#pop.append(this.#seg, this.#list);
    this.append(this.#button, this.#pop);
    if (this.#d) this.#render();
  }

  set data(value: QuickData) {
    this.#d = value;
    if (this.isConnected) this.#render();
  }

  #render(): void {
    const d = this.#d;
    if (!d || d.providers.length === 0) return;
    const cur = d.providers.find((p) => p.id === d.default) ?? d.providers[0];
    if (!cur) return;

    this.#button.disabled = false;
    this.#button.textContent = `${cur.name} · ${describeChoice(cur)}`;

    this.#seg.replaceChildren(
      ...d.providers.map((p) => {
        const b = el("button", "seg-btn", p.name);
        b.type = "button";
        b.setAttribute("role", "radio");
        b.setAttribute("aria-checked", String(p.id === cur.id));
        b.dataset["provider"] = p.id;
        return b;
      }),
    );
    this.#list.data = cur;
  }
}

customElements.define("ph-quick-model", PhQuickModel);
