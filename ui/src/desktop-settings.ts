// <ph-desktop>: computer use. Shows whether the desktop tool (agent-desktop) is installed,
// lets the user turn it on, and lists what the agent can do with it and what it asks first.
// Go owns the state and the approvals; this element only draws and sends the toggle.
import { el } from "./dom";
import { send } from "./protocol";

interface Skill {
  tool: string;
  label: string;
  class: "look" | "act";
  help: string;
}

interface DesktopData {
  enabled: boolean;
  found: boolean;
  path?: string;
  version?: string;
  problem?: string;
  skills: Skill[];
}

export class PhDesktop extends HTMLElement {
  readonly #status = el("p", "msg");
  readonly #toggle = el("input");
  readonly #toggleLabel = el("label", "mcp-use");
  readonly #skills = el("ul", "skill-list");
  readonly #install = el("div", "flex flex-col gap-2");
  #data: DesktopData = { enabled: false, found: false, skills: [] };

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#toggle.type = "checkbox";
    this.#toggle.setAttribute("aria-label", "Let the agent use the computer");
    this.#toggleLabel.append(this.#toggle, document.createTextNode("Let the agent use the computer"));
    this.#toggle.addEventListener("change", () => send("v", "desktop-enable", this.#toggle.checked ? "1" : "0"), { passive: true });

    const code = el("code", "path", "npm install -g agent-desktop");
    const recheck = el("button", "btn btn-sm", "Check again");
    recheck.type = "button";
    recheck.addEventListener("click", () => send("v", "desktop-refresh"), { passive: true });
    const row = el("div", "row");
    row.append(code, recheck);
    this.#install.append(
      el("p", "text-fg-muted", "Computer use needs agent-desktop, a small free tool that reads and operates Windows apps. Install it once, then check again."),
      row,
    );

    this.#status.setAttribute("aria-live", "polite");
    const how = el("div", "flex flex-col gap-1");
    how.append(
      el("h3", undefined, "How it asks"),
      el("p", "max-w-[62ch] text-fg-muted", "Looking at an app asks once per app each time you send a message. Anything that changes something, or reads your clipboard, asks every time, and you can allow all actions for that message. If you decline, the agent is told not to retry. You can stop at any time with the Stop button."),
    );
    const skills = el("div", "flex flex-col gap-3");
    skills.append(el("h3", undefined, "What the agent can do"), this.#skills);
    this.append(this.#toggleLabel, this.#status, this.#install, how, skills);
    this.#render();
  }

  set data(v: DesktopData) {
    this.#data = v;
    if (this.isConnected) this.#render();
  }

  #render(): void {
    const d = this.#data;
    this.#toggle.checked = d.enabled;
    this.#toggle.disabled = !d.found;
    this.#install.hidden = d.found;
    if (!d.found) {
      this.#status.className = "msg is-warn";
      this.#status.textContent = d.problem ?? "agent-desktop is not installed.";
    } else if (d.problem) {
      this.#status.className = "msg is-error";
      this.#status.textContent = d.problem;
    } else {
      this.#status.className = "msg is-ok";
      this.#status.textContent = `agent-desktop ${d.version ?? ""} is installed${d.enabled ? " and on. The agent can use it with Claude or an OpenAI-compatible API provider." : ". Turn it on to let the agent use it."}`;
    }
    this.#skills.replaceChildren(...d.skills.map((s) => {
      const li = el("li", "skill");
      const text = el("div", "flex min-w-0 flex-col");
      text.append(el("span", "font-medium", s.label), el("span", "text-xs text-fg-muted", s.help));
      const badge = el("span", `skill-badge ${s.class === "act" ? "is-act" : ""}`, s.class === "act" ? "Asks every time" : "Asks once per app");
      li.append(text, badge);
      return li;
    }));
  }
}

customElements.define("ph-desktop", PhDesktop);
