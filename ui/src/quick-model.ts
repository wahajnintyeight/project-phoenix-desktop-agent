// <ph-quick-model>: the model picker in the chat composer. One button shows the
// current provider and model; it opens a popover to switch provider, model and effort
// without going to Settings. It lists only providers that are ready to run, and shows the
// same data as the Settings rows.
import { icon } from "./icons";
import { send } from "./protocol";
import { describeChoice, type ModelsData, PhModelList } from "./model-list";

interface QuickData {
  default: string;
  providers: (ModelsData & { ready?: boolean })[];
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
  readonly #label = el("span", "qm-label", "Model");
  readonly #pop = el("div", "qm-pop");
  readonly #seg = el("div", "seg");
  readonly #list = document.createElement("ph-model-list") as PhModelList;
  readonly #empty = el("p", "qm-empty", "No provider is ready yet. Sign in or add an API key in Settings.");
  readonly #manage = el("button", "btn btn-quiet", "Manage providers…");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#pop.id = "quick-model-pop";
    this.#pop.setAttribute("popover", "");
    this.#button.type = "button";
    this.#button.setAttribute("popovertarget", this.#pop.id);
    this.#button.setAttribute("aria-label", "Choose model");
    this.#button.disabled = true;
    this.#button.append(this.#label, icon("chevronDown", 12));

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

    // Opening Settings is a normal action; the popover is closed first so it does not linger.
    this.#manage.type = "button";
    this.#manage.dataset["action"] = "menu.settings";
    this.#manage.addEventListener("click", () => this.#pop.hidePopover(), { passive: true });
    const foot = el("div", "qm-foot");
    foot.append(this.#manage);

    this.#list.classList.add("compact");
    this.#pop.append(this.#seg, this.#list, this.#empty, foot);
    this.append(this.#button, this.#pop);
    if (this.#d) this.#render();
  }

  set data(value: QuickData) {
    this.#d = value;
    if (this.isConnected) this.#render();
  }

  #render(): void {
    const d = this.#d;
    if (!d) return;
    // The default provider is always offered, so the current choice is never hidden.
    const usable = d.providers.filter((p) => p.ready === true || p.id === d.default);
    const cur = usable.find((p) => p.id === d.default) ?? usable[0];
    const readyCount = d.providers.filter((p) => p.ready === true).length;

    this.#button.disabled = false;
    this.#label.textContent = cur && readyCount > 0 ? `${cur.name} · ${describeChoice(cur)}` : "Choose a model";
    this.#empty.hidden = readyCount > 0;
    this.#list.hidden = readyCount === 0 || !cur;
    this.#seg.hidden = usable.length < 2 || readyCount === 0;

    this.#seg.replaceChildren(
      ...usable.map((p) => {
        const b = el("button", "seg-btn", p.name);
        b.type = "button";
        b.setAttribute("role", "radio");
        b.setAttribute("aria-checked", String(p.id === cur?.id));
        b.dataset["provider"] = p.id;
        return b;
      }),
    );
    if (cur) this.#list.data = cur;

    // The start page shows whether the agent has anything to run on.
    document.dispatchEvent(new CustomEvent("quickchange", {
      detail: { ready: readyCount, current: cur ? `${cur.name} · ${describeChoice(cur)}` : "" },
    }));
  }
}

customElements.define("ph-quick-model", PhQuickModel);
