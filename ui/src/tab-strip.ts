// <ph-tab-strip>: renders the tab list Go sends. Holds no tab state of its own beyond
// the last list it was given; selection, ordering and closing are decided in Go.

import { icon } from "./icons";

export interface TabInfo {
  id: number;
  title: string;
  url: string;
  kind: "home" | "web" | "settings" | "files" | "tasks";
  active: boolean;
  loading: boolean;
}

export class PhTabStrip extends HTMLElement {
  #tabs: TabInfo[] = [];
  readonly #els = new Map<number, HTMLElement>();

  get tabs(): TabInfo[] {
    return this.#tabs;
  }

  set tabs(value: TabInfo[]) {
    this.#tabs = value;
    this.#render();
  }

  #render(): void {
    const seen = new Set<number>();
    let prev: Element | null = null;

    for (const tab of this.#tabs) {
      seen.add(tab.id);
      let el = this.#els.get(tab.id);
      if (!el) {
        el = this.#create(tab.id);
        this.#els.set(tab.id, el);
      }
      this.#update(el, tab);

      const expected: Element | null = prev ? prev.nextElementSibling : this.firstElementChild;
      if (expected !== el) this.insertBefore(el, expected);
      prev = el;
    }

    for (const [id, el] of this.#els) {
      if (!seen.has(id)) {
        el.remove();
        this.#els.delete(id);
      }
    }
  }

  #create(id: number): HTMLElement {
    const el = document.createElement("div");
    el.className = "tab";
    el.setAttribute("role", "tab");
    el.dataset["id"] = String(id);

    const main = document.createElement("button");
    main.className = "tab-main";
    main.dataset["action"] = "tab.select";
    main.dataset["id"] = String(id);
    const spin = document.createElement("span");
    spin.className = "tab-spin";
    spin.setAttribute("aria-hidden", "true");
    const title = document.createElement("span");
    title.className = "tab-title";
    main.append(spin, title);

    const close = document.createElement("button");
    close.className = "tab-close";
    close.dataset["action"] = "tab.close";
    close.dataset["id"] = String(id);
    close.setAttribute("aria-label", "Close tab");
    close.append(icon("close", 12));

    el.append(main, close);
    return el;
  }

  #update(el: HTMLElement, tab: TabInfo): void {
    el.setAttribute("aria-selected", String(tab.active));
    el.dataset["loading"] = String(tab.loading);
    el.title = tab.title;
    const title = el.querySelector(".tab-title");
    if (title && title.textContent !== tab.title) title.textContent = tab.title;
  }
}

customElements.define("ph-tab-strip", PhTabStrip);
