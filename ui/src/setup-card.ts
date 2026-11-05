// <ph-setup-card>: shown in the empty chat when no AI provider can run yet. Phoenix uses the
// user's own account: a Claude or Codex sign-in that already exists is found by itself (the
// command-line client says so; Phoenix never reads its credential files), otherwise the user
// signs in on the web through that client, or pastes an API key. It hides itself the moment
// any provider is ready, so there is nothing to dismiss.
import { el } from "./dom";
import { icon } from "./icons";
import { send } from "./protocol";
import type { ProviderState } from "./quick-model";

interface Row { title: string; note: string; action: () => void }

export class PhSetupCard extends HTMLElement {
  #providers: ProviderState[] = [];
  #lastCheck = 0;

  connectedCallback(): void {
    this.hidden = true; // until Go reports that nothing is ready
    document.addEventListener("quickchange", this.#onChange);
    // A user who just installed a client comes back to the window; look again (it runs the
    // clients, so not more than every few seconds).
    window.addEventListener("focus", this.#onFocus);
  }

  disconnectedCallback(): void {
    document.removeEventListener("quickchange", this.#onChange);
    window.removeEventListener("focus", this.#onFocus);
  }

  readonly #onChange = (e: Event): void => {
    const d = (e as CustomEvent<{ ready: number; providers?: ProviderState[] }>).detail;
    this.#providers = d.providers ?? [];
    this.hidden = d.ready > 0 || this.#providers.length === 0;
    if (!this.hidden) this.#render();
  };

  readonly #onFocus = (): void => {
    if (this.hidden || Date.now() - this.#lastCheck < 5000) return;
    this.#lastCheck = Date.now();
    send("v", "refresh");
  };

  #cli(id: string, name: string, signIn: string, account: string, getName: string): Row {
    const p = this.#providers.find((x) => x.id === id);
    if (p?.installed) {
      return { title: signIn, note: `Opens ${name}'s own sign-in in your browser. ${account}`, action: () => send("v", "login", id) };
    }
    return { title: `Get ${getName}`, note: `Not found on this PC. Install it, come back, and Phoenix will find it. ${account}`, action: () => send("c", "setup.get", id) };
  }

  #render(): void {
    const rows: Row[] = [
      this.#cli("claude", "Claude Code", "Sign in with Claude", "Uses your Claude subscription.", "Claude Code"),
      this.#cli("codex", "Codex", "Sign in with ChatGPT", "Uses your ChatGPT plan.", "Codex"),
      { title: "Use an API key", note: "Gemini, OpenRouter, Mistral, Z.ai or Grok. Nothing to install.", action: () => send("c", "menu.settings") },
    ];
    const list = el("div", "setup-rows");
    for (const r of rows) {
      const b = el("button", "setup-row");
      b.type = "button";
      const text = el("span", "setup-text");
      text.append(el("strong", undefined, r.title), el("span", undefined, r.note));
      b.append(text, icon("chevronRight", 14));
      b.addEventListener("click", r.action, { passive: true });
      list.append(b);
    }
    this.replaceChildren(
      el("h2", undefined, "Connect a model to start"),
      el("p", undefined, "Phoenix works with your own AI account. If you are already signed in to Claude Code or Codex, it is picked up automatically."),
      list,
    );
  }
}

customElements.define("ph-setup-card", PhSetupCard);
