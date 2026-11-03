// <ph-quick-actions>: the bar of one-click requests under the page. Go chooses the buttons
// from what the tab looks like (`actions`) and this element only draws them; a click sends
// the action's id back ("g act <id>") and Go words the request. Labels are placed with
// textContent: they come from Go, but the habit is cheap.
import { el } from "./dom";
import { icon } from "./icons";
import { send } from "./protocol";

export interface QuickAction {
  id: string;
  label: string;
  primary?: boolean;
}

export interface QuickActionSet {
  context: string;
  items: QuickAction[];
}

export class PhQuickActions extends HTMLElement {
  readonly #context = el("span", "qa-context");
  readonly #list = el("div", "qa-list");
  #busy = false;
  #probeAt = 0;

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.setAttribute("role", "toolbar");
    this.setAttribute("aria-label", "Quick actions");

    const lead = el("div", "qa-lead");
    lead.append(icon("bolt", 14), this.#context);
    this.append(lead, this.#list);

    this.#list.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLButtonElement>("button[data-act]");
      if (!b || b.disabled) return;
      send("g", "act", b.dataset["act"] ?? "");
    }, { passive: true });

    // The user may have just selected something on the page. Reaching for the bar is the cue
    // to look again; Go ignores an unchanged answer, so nothing flickers.
    this.addEventListener("pointerenter", () => {
      const now = performance.now();
      if (now - this.#probeAt < 400) return;
      this.#probeAt = now;
      send("g", "probe");
    }, { passive: true });
  }

  set actions(set: QuickActionSet) {
    this.#context.textContent = set.context;
    const buttons = set.items.map((a) => {
      const b = el("button", "qa-chip", a.label);
      b.type = "button";
      b.dataset["act"] = a.id;
      if (a.primary) b.dataset["primary"] = "true";
      b.disabled = this.#busy;
      return b;
    });
    this.#list.replaceChildren(...buttons);
  }

  /** While a reply is on its way, the buttons wait: one request at a time. */
  set disabled(busy: boolean) {
    this.#busy = busy;
    this.toggleAttribute("data-busy", busy);
    this.#list.querySelectorAll("button").forEach((b) => { b.disabled = busy; });
  }
}

customElements.define("ph-quick-actions", PhQuickActions);
