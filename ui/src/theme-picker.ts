// <ph-theme-picker>: chooses the palette and the mode. Each palette is drawn as a small preview
// in its own colours for the mode in effect, so what you pick is what you see. The logic lives in
// themes.ts; this element only draws and sends the choice there.
import { el } from "./dom";
import { icon } from "./icons";
import { choice, effectiveMode, type Mode, type ModeChoice, onTheme, PALETTES, setChoice, swatch } from "./themes";

const MODES: { id: ModeChoice; label: string }[] = [
  { id: "dark", label: "Dark" },
  { id: "light", label: "Light" },
  { id: "system", label: "Match Windows" },
];

export class PhThemePicker extends HTMLElement {
  readonly #seg = el("div", "seg");
  readonly #grid = el("div", "theme-grid");

  connectedCallback(): void {
    if (this.childElementCount > 0) return;
    this.#seg.setAttribute("role", "radiogroup");
    this.#seg.setAttribute("aria-label", "Mode");
    this.#grid.setAttribute("role", "radiogroup");
    this.#grid.setAttribute("aria-label", "Color theme");

    const modeRow = el("div", "theme-row");
    modeRow.append(el("span", "font-medium", "Mode"), this.#seg);
    this.append(modeRow, this.#grid);

    this.#seg.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-mode]");
      if (b) setChoice({ mode: b.dataset["mode"] as ModeChoice });
    }, { passive: true });
    this.#grid.addEventListener("click", (e) => {
      const b = (e.target as Element | null)?.closest<HTMLElement>("[data-theme]");
      if (b) setChoice({ theme: b.dataset["theme"] ?? "" });
    }, { passive: true });
    // Arrow keys move between the cards, as in any radio group.
    this.#grid.addEventListener("keydown", (e) => {
      if (!["ArrowRight", "ArrowDown", "ArrowLeft", "ArrowUp"].includes(e.key)) return;
      e.preventDefault();
      const i = PALETTES.findIndex((p) => p.id === choice().theme);
      const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : -1;
      const next = PALETTES[(i + step + PALETTES.length) % PALETTES.length];
      if (next) {
        setChoice({ theme: next.id });
        this.#grid.querySelector<HTMLElement>(`[data-theme="${next.id}"]`)?.focus();
      }
    });

    onTheme(() => this.#render());
    this.#render();
  }

  #render(): void {
    const c = choice();
    const mode = effectiveMode(c);

    this.#seg.replaceChildren(...MODES.map((m) => {
      const b = el("button", "seg-btn", m.label);
      b.type = "button";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(c.mode === m.id));
      b.dataset["mode"] = m.id;
      return b;
    }));

    this.#grid.replaceChildren(...PALETTES.map((p) => {
      const on = p.id === c.theme;
      const b = el("button", "theme-card");
      b.type = "button";
      b.setAttribute("role", "radio");
      b.setAttribute("aria-checked", String(on));
      b.tabIndex = on ? 0 : -1;
      b.dataset["theme"] = p.id;
      b.append(this.#preview(p.id, mode), this.#label(p.name, p.note, on));
      return b;
    }));
  }

  #preview(id: string, mode: Mode): HTMLElement {
    const p = PALETTES.find((x) => x.id === id) ?? PALETTES[0];
    if (!p) return el("div");
    const s = swatch(p, mode);
    const box = el("div", "theme-preview");
    box.style.setProperty("--pv-bg", s.bg);
    box.style.setProperty("--pv-panel", s.panel);
    box.style.setProperty("--pv-accent", s.accent);
    box.style.setProperty("--pv-fg", s.fg);
    box.style.setProperty("--pv-line", s.line);
    box.setAttribute("aria-hidden", "true");
    const side = el("span", "pv-side");
    const body = el("span", "pv-body");
    body.append(el("i", "pv-line pv-line-a"), el("i", "pv-line pv-line-b"), el("i", "pv-pill"));
    box.append(side, body);
    return box;
  }

  #label(name: string, note: string, on: boolean): HTMLElement {
    const row = el("span", "theme-label");
    const text = el("span", "theme-text");
    text.append(el("strong", undefined, name), el("span", undefined, note));
    row.append(text);
    if (on) {
      const tick = el("span", "theme-tick");
      tick.append(icon("check", 12));
      row.append(tick);
    }
    return row;
  }
}

customElements.define("ph-theme-picker", PhThemePicker);
