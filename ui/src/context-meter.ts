// <ph-context>: how much of the model's context window is left, always visible at the top of
// the agent panel. Go sends the estimate (characters / 4) after every change to the conversation.
import { el } from "./dom";

interface Data {
  used: number;
  window: number;
  compact: number;
}

function k(n: number): string {
  return n >= 1000 ? `${Math.round(n / 100) / 10}k`.replace(".0k", "k") : String(n);
}

class PhContext extends HTMLElement {
  readonly #left = el("span", "ctx-left");
  readonly #bar = el("span", "ctx-fill");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    const row = el("div", "ctx-row");
    row.append(el("span", "ctx-label", "Context"), this.#left);
    const track = el("div", "ctx-track");
    track.append(this.#bar);
    this.setAttribute("role", "meter");
    this.append(row, track);
    this.data = { used: 0, window: 256000, compact: 230000 };
  }

  set data(d: Data) {
    const window = d.window > 0 ? d.window : 256000;
    const used = Math.min(Math.max(d.used, 0), window);
    const left = window - used;
    const share = used / window;
    this.#left.textContent = `${k(left)} left`;
    this.#bar.style.width = `${Math.max(share * 100, used > 0 ? 1.5 : 0)}%`;
    const state = d.compact > 0 && used >= d.compact * 0.92 ? "full" : share >= 0.6 ? "high" : "ok";
    this.dataset["state"] = state;
    this.title = `${k(used)} of ${k(window)} tokens used. At ${k(d.compact)} Phoenix summarizes the chat and continues in a new session.`;
    this.setAttribute("aria-valuemin", "0");
    this.setAttribute("aria-valuemax", String(window));
    this.setAttribute("aria-valuenow", String(used));
    this.setAttribute("aria-label", `Context window: ${k(left)} left`);
  }
}

customElements.define("ph-context", PhContext);
