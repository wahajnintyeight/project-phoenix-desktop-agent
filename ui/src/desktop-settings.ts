// <ph-desktop>: native computer-use capabilities and their on/off toggle.
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
  skills: Skill[];
  allowed: { server: string; tool: string }[];
}

export class PhDesktop extends HTMLElement {
  readonly #status = el("p", "msg");
  readonly #toggle = el("input");
  readonly #toggleLabel = el("label", "mcp-use");
  readonly #skills = el("ul", "skill-list");
  readonly #permissions = el("ul", "skill-list");
  #data: DesktopData = { enabled: false, skills: [], allowed: [] };

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#toggle.type = "checkbox";
    this.#toggle.setAttribute("aria-label", "Let the agent use the computer");
    this.#toggleLabel.append(this.#toggle, document.createTextNode("Let the agent use the computer"));
    this.#toggle.addEventListener("change", () => send("v", "desktop-enable", this.#toggle.checked ? "1" : "0"), { passive: true });

    this.#status.setAttribute("aria-live", "polite");
    const how = el("div", "flex flex-col gap-1");
    how.append(
      el("h3", undefined, "How it works"),
      el("p", "max-w-[62ch] text-fg-muted", "When computer use is on, Phoenix can inspect and control apps automatically while it works on your request. You can follow its actions in Activity and stop it at any time."),
    );
    const skills = el("div", "flex flex-col gap-3");
    skills.append(el("h3", undefined, "What the agent can do"), this.#skills);
    const permissions = el("div", "flex flex-col gap-3");
    permissions.append(el("h3", undefined, "Always allowed connected tools"), el("p", "text-fg-muted", "These tools can run in future messages without asking. Revoke access at any time."), this.#permissions);
    this.append(this.#toggleLabel, this.#status, how, skills, permissions);
    this.#render();
  }

  set data(v: DesktopData) {
    this.#data = v;
    if (this.isConnected) this.#render();
  }

  #render(): void {
    const d = this.#data;
    this.#toggle.checked = d.enabled;
    this.#toggle.disabled = false;
    this.#status.className = "msg is-ok";
    this.#status.textContent = d.enabled ? "Native computer use is on. Actions run automatically." : "Native computer use is off.";
    this.#skills.replaceChildren(...d.skills.map((s) => {
      const li = el("li", "skill");
      const text = el("div", "flex min-w-0 flex-col");
      text.append(el("span", "font-medium", s.label), el("span", "text-xs text-fg-muted", s.help));
      const badge = el("span", "skill-badge", "Automatic");
      li.append(text, badge);
      return li;
    }));
    const allowed = d.allowed ?? [];
    this.#permissions.replaceChildren(...(allowed.length ? allowed.map((p) => {
      const li = el("li", "skill");
      const label = el("span", "font-medium", `${p.server} › ${p.tool}`);
      const revoke = el("button", "btn btn-sm", "Revoke");
      revoke.type = "button";
      revoke.setAttribute("aria-label", `Revoke ${p.server} ${p.tool}`);
      revoke.addEventListener("click", () => send("v", "desktop-revoke", p.server, p.tool));
      li.append(label, revoke);
      return li;
    }) : [el("li", "text-fg-muted", "No tools always allowed.")]));
  }
}

customElements.define("ph-desktop", PhDesktop);
