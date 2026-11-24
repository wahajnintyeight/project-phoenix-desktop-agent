import { el } from "./dom";
import { icon } from "./icons";
import type { ChatEvent } from "./chat";

/**
 * Consecutive tool calls fold into one collapsed row, so a long run reads as a single line of
 * progress ("Clicking Apply · 7 steps") instead of a wall of cards. Opening it shows each step.
 */
export class StepGroup {
  readonly box = el("details", "chat-steps") as HTMLDetailsElement;
  readonly #label = el("span", "chat-steps-label");
  readonly #count = el("span", "chat-steps-count");
  readonly body = el("div", "chat-steps-body");
  #total = 0;
  #failed = 0;
  #latest = "";
  /** Live until the run ends: between two tool calls the agent is still working, so the row
   *  keeps naming the latest step instead of flipping to "Used N tools" and back. */
  #live = true;

  constructor() {
    const summary = document.createElement("summary");
    const chevron = icon("chevronRight", 14);
    chevron.classList.add("chat-steps-chevron");
    summary.append(el("span", "chat-spinner"), this.#label, this.#count, chevron);
    this.box.append(summary, this.body);
    this.#render();
  }

  start(title: string): void {
    this.#total++;
    this.#latest = title;
    this.#render();
  }

  finish(failed: boolean): void {
    if (failed) this.#failed++;
    this.#render();
  }

  /** The run ended: nothing is running any more, whatever Go last reported. */
  settle(): void {
    this.#live = false;
    this.#render();
  }

  #render(): void {
    const running = this.#live;
    this.box.dataset["state"] = running ? "running" : this.#failed ? "failed" : "done";
    this.#label.textContent = running ? this.#latest || "Working…" : `Used ${this.#total === 1 ? "1 tool" : `${this.#total} tools`}`;
    const bits = running ? [`${this.#total} ${this.#total === 1 ? "step" : "steps"}`] : [];
    if (this.#failed) bits.push(`${this.#failed} failed`);
    this.#count.textContent = bits.join(" · ");
    this.#count.hidden = bits.length === 0;
  }
}

export function prettyToolText(value: string): string {
  try { return JSON.stringify(JSON.parse(value), null, 2) ?? value; } catch { return value; }
}

/** Plain wording for Phoenix's own browser tools, so a step reads as an action, not a function name. */
const BROWSER_STEPS: Record<string, string> = {
  browser_tabs: "Checking open tabs", browser_read: "Reading the page", browser_snapshot: "Looking at the page's controls",
  browser_screenshot: "Taking a screenshot", browser_click: "Clicking", browser_type: "Typing", browser_fill: "Filling in a field",
  browser_select: "Choosing an option", browser_press: "Pressing a key", browser_scroll: "Scrolling", browser_drag: "Dragging",
  browser_wait: "Waiting for the page", browser_navigate: "Opening a page", browser_open_tab: "Opening a tab",
  browser_close_tab: "Closing a tab", browser_switch_tab: "Switching tab", browser_back: "Going back",
  browser_forward: "Going forward", browser_reload: "Reloading the page", browser_copy: "Copying", browser_paste: "Pasting",
};

export function stepTitle(ev: ChatEvent): string {
  return (/browser/i.test(ev.server ?? "") && ev.toolName && BROWSER_STEPS[ev.toolName]) || ev.text || ev.toolName || "Working…";
}

