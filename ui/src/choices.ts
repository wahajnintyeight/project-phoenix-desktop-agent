// Choices: when the agent needs the user to pick, it ends its reply with a fenced block
//
//   ```choices
//   Which one first?
//   - Lorven Tech: Fullstack (PHP)
//   - RemoteHunter: Senior Full-Stack
//   ```
//
// Phoenix hides the block and draws the options as buttons under the message, plus a field for
// a different answer. A click (or typing and pressing the button) sends that text as the
// user's next message, so answering is one tap instead of writing. The block is plain text, so
// it works with every provider and survives being saved in the history.
import { el } from "./dom";
import { icon } from "./icons";

export interface ChoiceSet {
  question: string;
  options: string[];
}

const MAX_OPTIONS = 8;
const MAX_LABEL = 120;
const COMPLETE = /(?:^|\n)[ \t]*```choices[ \t]*\n([\s\S]*?)\n[ \t]*```[ \t]*(?:\n|$)/;
const OPEN = /(?:^|\n)[ \t]*```choices[\s\S]*$/;

/** Splits a reply into the text to show and the choices to draw (null when there are none). */
export function splitChoices(raw: string): { body: string; choices: ChoiceSet | null } {
  const m = COMPLETE.exec(raw);
  if (m) {
    const body = (raw.slice(0, m.index) + "\n" + raw.slice(m.index + m[0].length)).trim();
    return { body, choices: parse(m[1] ?? "") };
  }
  // Still being written: keep the half-finished block off the screen.
  const open = OPEN.exec(raw);
  return { body: open ? raw.slice(0, open.index).trimEnd() : raw, choices: null };
}

function parse(block: string): ChoiceSet | null {
  const options: string[] = [];
  let question = "";
  for (const line of block.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const item = /^(?:[-*•]|\d+[.)])\s+(.*)$/.exec(t);
    if (item) {
      const label = (item[1] ?? "").replace(/\*\*/g, "").trim().slice(0, MAX_LABEL);
      if (label && options.length < MAX_OPTIONS && !options.includes(label)) options.push(label);
    } else if (!question && options.length === 0) {
      question = t.replace(/^\?\s*/, "").slice(0, 160);
    }
  }
  return options.length ? { question, options } : null;
}

/** The panel under a message: one button per option and a box for something else. */
export function choicePanel(set: ChoiceSet, choose: (text: string) => void): HTMLElement {
  const panel = el("div", "chat-choices");
  panel.setAttribute("role", "group");
  panel.setAttribute("aria-label", set.question || "Choose an option");
  if (set.question) panel.append(el("p", "chat-choices-question", set.question));

  const list = el("div", "chat-choices-list");
  const finish = (text: string, button?: HTMLElement): void => {
    if (panel.dataset["answered"] !== undefined) return;
    panel.dataset["answered"] = "";
    panel.querySelectorAll<HTMLButtonElement | HTMLInputElement>("button, input").forEach((x) => { x.disabled = true; });
    button?.setAttribute("aria-pressed", "true");
    choose(text);
  };
  for (const option of set.options) {
    const b = el("button", "btn btn-sm chat-choice", option);
    b.type = "button";
    b.addEventListener("click", () => finish(option, b), { passive: true });
    list.append(b);
  }
  panel.append(list);

  const custom = el("form", "chat-choices-custom");
  const input = el("input", "chat-choices-input");
  input.type = "text";
  input.maxLength = 500;
  input.placeholder = "Or type your own answer";
  input.setAttribute("aria-label", "Your own answer");
  const go = el("button", "btn btn-primary btn-sm chat-choices-go");
  go.type = "submit";
  go.disabled = true;
  go.setAttribute("aria-label", "Send this answer");
  go.append(icon("arrowUp", 14));
  input.addEventListener("input", () => { go.disabled = input.value.trim() === ""; }, { passive: true });
  custom.addEventListener("submit", (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (text) finish(text);
  });
  custom.append(input, go);
  panel.append(custom);
  return panel;
}
