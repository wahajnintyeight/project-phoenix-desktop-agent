// <ph-quick-model>: the model picker in the chat composer. One button shows the
// current provider and model; it opens a popover to switch provider, model and effort
// without going to Settings. It lists only providers that are ready to run, and shows the
// same data as the Settings rows.
import { icon } from "./icons";
import { send } from "./protocol";
import { describeChoice, type ModelsData, PhModelList } from "./model-list";
import { handleRadioKeys } from "./radio-navigation";

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
  readonly #seg = el("div", "qm-providers scroll");
  readonly #list = document.createElement("ph-model-list") as PhModelList;
  readonly #empty = el("p", "qm-empty", "No provider is ready yet. Sign in or add an API key in Settings.");
  readonly #manage = el("button", "btn btn-quiet", "Manage providers…");
  readonly #position = (): void => {
    const button = this.#button.getBoundingClientRect();
    const panelTop = this.closest<HTMLElement>(".agent")?.getBoundingClientRect().top ?? 0;
    this.#pop.style.setProperty("--qm-bottom", `${Math.max(8, window.innerHeight - button.top + 8)}px`);
    this.#pop.style.setProperty("--qm-max-height", `${Math.max(0, button.top - panelTop - 20)}px`);
  };

  connectedCallback(): void {
    if (this.childElementCount > 0) {
      window.addEventListener("resize", this.#position);
      return;
    }
    this.#pop.id = "quick-model-pop";
    this.#pop.setAttribute("popover", "");
    this.#button.type = "button";
    this.#button.setAttribute("popovertarget", this.#pop.id);
    this.#button.setAttribute("aria-label", "Choose model");
    this.#button.setAttribute("aria-expanded", "false");
    this.#button.disabled = true;
    this.#button.append(this.#label, icon("chevronDown", 12));
    this.#pop.addEventListener("beforetoggle", (event) => {
      if ((event as ToggleEvent).newState === "open") {
        this.#position();
      }
    });
    window.addEventListener("resize", this.#position);
    this.#pop.addEventListener("toggle", (event) => {
      this.#button.setAttribute("aria-expanded", String((event as ToggleEvent).newState === "open"));
    });

    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "Provider");
    this.#seg.addEventListener("keydown", handleRadioKeys);
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

    const heading = el("div", "qm-heading");
    const title = el("div", "qm-heading-copy");
    title.append(el("h3", "qm-title", "Choose a model"));
    const close = el("button", "qm-close");
    close.type = "button";
    close.setAttribute("aria-label", "Close model picker");
    close.append(icon("close", 15));
    close.addEventListener("click", () => this.#pop.hidePopover(), { passive: true });
    heading.append(title, close);
    const providerLabel = el("p", "qm-section-label", "Provider");
    const providerSection = el("div", "qm-provider-section");
    providerSection.append(providerLabel, this.#seg);
    const models = el("div", "qm-models");
    models.append(el("p", "qm-section-label", "Model"), this.#list, this.#empty);
    this.#list.classList.add("compact");
    this.#pop.append(heading, providerSection, models, foot);
    this.append(this.#button, this.#pop);
    if (this.#d) this.#render();
  }

  disconnectedCallback(): void {
    window.removeEventListener("resize", this.#position);
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
    const focused = document.activeElement instanceof HTMLElement && this.#seg.contains(document.activeElement)
      ? document.activeElement.dataset["provider"] : undefined;

    this.#button.disabled = false;
    this.#label.textContent = cur && readyCount > 0 ? `${cur.name} · ${describeChoice(cur)}` : "Choose a model";
    this.#empty.hidden = readyCount > 0;
    this.#list.hidden = readyCount === 0 || !cur;
    this.#seg.parentElement!.hidden = readyCount === 0;

    this.#seg.replaceChildren(
      ...usable.map((p) => {
        const b = el("button", "qm-provider", p.name);
        b.type = "button";
        b.setAttribute("role", "radio");
        b.setAttribute("aria-checked", String(p.id === cur?.id));
        b.tabIndex = p.id === cur?.id ? 0 : -1;
        b.dataset["provider"] = p.id;
        return b;
      }),
    );
    if (focused) Array.from(this.#seg.querySelectorAll<HTMLButtonElement>("button[data-provider]"))
      .find((button) => button.dataset["provider"] === focused)?.focus();
    if (cur) this.#list.data = cur;

    // The start page shows whether the agent has anything to run on.
    document.dispatchEvent(new CustomEvent("quickchange", {
      detail: { ready: readyCount, current: cur ? `${cur.name} · ${describeChoice(cur)}` : "" },
    }));
  }
}

customElements.define("ph-quick-model", PhQuickModel);
