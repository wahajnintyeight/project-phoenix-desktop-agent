// Themes: every palette comes in a dark and a light mode, and the mode can follow Windows.
//
// A palette is two small ramps, a neutral one for surfaces and text and an accent one, each nine
// steps from darkest to lightest. The roles the stylesheet uses (--color-canvas, --color-fg,
// --color-accent …) are derived from the ramps by fixed rules, then written once onto <html>.
// Nothing here runs after that: switching is one pass over ~45 properties, and the stylesheet
// stays plain CSS variables. A palette may override any role where the rules would read badly.

export type Mode = "dark" | "light";
export type ModeChoice = Mode | "system";

type Ramp = readonly [string, string, string, string, string, string, string, string, string];

interface Palette {
  id: string;
  name: string;
  note: string;
  /** Surfaces and text, darkest to lightest, for each mode. */
  n: { dark: Ramp; light: Ramp };
  /** Accent, darkest to lightest, for each mode. */
  a: { dark: Ramp; light: Ramp };
  /** Roles to set by hand instead of deriving, per mode (CSS custom property names without --color-). */
  over?: { dark?: Record<string, string>; light?: Record<string, string> };
}

export const PALETTES: readonly Palette[] = [
  {
    id: "mauve",
    name: "Mauve shadow",
    note: "Plum and rose",
    n: {
      dark: ["#150e12", "#1f141a", "#3d2934", "#5c3d4e", "#7a5268", "#b690a5", "#d6c2cd", "#ebe0e6", "#f5f0f3"],
      light: ["#2a1b23", "#5c3d4e", "#7a5268", "#a6839a", "#d6c2cd", "#e6d8e0", "#f1e8ed", "#f8f3f6", "#fdfbfc"],
    },
    a: {
      dark: ["#150e12", "#3d2934", "#5c3d4e", "#7a5268", "#996682", "#ad859b", "#c2a3b4", "#d6c2cd", "#ebe0e6"],
      light: ["#2a1b23", "#4a2f3e", "#6b4659", "#7a5268", "#996682", "#ad859b", "#c2a3b4", "#d6c2cd", "#ebe0e6"],
    },
  },
  {
    id: "honey",
    name: "Honey garden",
    note: "Honey, linen and teal",
    n: {
      dark: ["#0e1513", "#141d1b", "#243430", "#3a5149", "#65887f", "#9cb6b0", "#cedbd7", "#e6edeb", "#f7ede2"],
      light: ["#4a3014", "#6b4a27", "#956129", "#cf904e", "#e3bf99", "#f0dcc6", "#f7ede2", "#fbf6ef", "#fdfbf9"],
    },
    a: {
      dark: ["#412904", "#815308", "#c27c0b", "#f2a11f", "#f6bd60", "#f8ca80", "#f9d7a0", "#fbe4bf", "#fdf2df"],
      light: ["#412904", "#5f3d06", "#815308", "#c27c0b", "#f2a11f", "#f6bd60", "#f8ca80", "#f9d7a0", "#fbe4bf"],
    },
    over: {
      dark: { danger: "#f28482", "danger-ink": "#430807", ok: "#84a59d" },
      light: { danger: "#b3201d", "danger-ink": "#fce6e6", ok: "#3f6a60" },
    },
  },
  {
    id: "sand",
    name: "Soft sand",
    note: "Warm, quiet browns",
    n: {
      dark: ["#1a1713", "#241f19", "#352d24", "#4d4234", "#8c7a63", "#b9a58f", "#d9c8b4", "#ece0d1", "#f7efe6"],
      light: ["#34342a", "#625241", "#7d6650", "#b69f89", "#d5bdaf", "#e3d5ca", "#edede9", "#f5ebe0", "#fdfbf9"],
    },
    a: {
      dark: ["#2c1f12", "#664735", "#775b3c", "#986b50", "#c78f52", "#d9ae7e", "#e6c8a4", "#f0dcc6", "#f7ece0"],
      light: ["#2c1f12", "#473017", "#664735", "#775b3c", "#986b50", "#c78f52", "#d9ae7e", "#e6c8a4", "#f0dcc6"],
    },
  },
  {
    id: "earth",
    name: "Neutral earth",
    note: "Stone and clay",
    n: {
      dark: ["#14110e", "#1c1814", "#2c2620", "#463d33", "#786c60", "#a89b8c", "#d1c5b6", "#e6dccf", "#f4eee6"],
      light: ["#0a0908", "#423b35", "#625548", "#9b8a76", "#c6ac8f", "#ddcebc", "#eae0d5", "#efe7de", "#fbf9f7"],
    },
    a: {
      dark: ["#2a2118", "#4c4133", "#5e503f", "#786c60", "#89755b", "#a89177", "#c6ac8f", "#ddcebc", "#efe7de"],
      light: ["#2a2118", "#393026", "#4c4133", "#5e503f", "#786c60", "#89755b", "#a89177", "#c6ac8f", "#ddcebc"],
    },
  },
  {
    id: "steel",
    name: "Steel",
    note: "Cool greys",
    n: {
      dark: ["#0e1012", "#15181b", "#23282d", "#3a4148", "#6c757d", "#9aa3ab", "#c4cbd2", "#e2e6ea", "#f4f6f8"],
      light: ["#212529", "#495057", "#5c656d", "#868e96", "#adb5bd", "#dee2e6", "#e9ecef", "#f3f4f6", "#fefefe"],
    },
    a: {
      dark: ["#212529", "#343a40", "#495057", "#6c757d", "#868e96", "#adb5bd", "#ced4da", "#dee2e6", "#f1f3f5"],
      light: ["#212529", "#2f353b", "#343a40", "#495057", "#6c757d", "#868e96", "#adb5bd", "#ced4da", "#dee2e6"],
    },
  },
  {
    id: "basic",
    name: "Basic",
    note: "Black, charcoal and white",
    n: {
      dark: ["#000000", "#0f0f0f", "#1e1e1e", "#3b3b3b", "#6e6e6e", "#929292", "#b7b7b7", "#dbdbdb", "#ffffff"],
      light: ["#0f0f0f", "#333333", "#494949", "#929292", "#b7b7b7", "#dbdbdb", "#ececec", "#f5f5f5", "#ffffff"],
    },
    a: {
      dark: ["#000000", "#333333", "#b7b7b7", "#dbdbdb", "#ebebeb", "#f2f2f2", "#f8f8f8", "#fcfcfc", "#ffffff"],
      light: ["#000000", "#0f0f0f", "#1e1e1e", "#494949", "#6e6e6e", "#929292", "#b7b7b7", "#dbdbdb", "#ffffff"],
    },
  },
];

// ── colour maths ──────────────────────────────────────────────────────────

type RGB = [number, number, number];

function rgb(hex: string): RGB {
  const h = hex.replace("#", "");
  return [parseInt(h.slice(0, 2), 16), parseInt(h.slice(2, 4), 16), parseInt(h.slice(4, 6), 16)];
}

function hex(c: RGB): string {
  return "#" + c.map((v) => Math.max(0, Math.min(255, Math.round(v))).toString(16).padStart(2, "0")).join("");
}

/** t of the way from a to b. */
export function mix(a: string, b: string, t: number): string {
  const x = rgb(a);
  const y = rgb(b);
  return hex([x[0] + (y[0] - x[0]) * t, x[1] + (y[1] - x[1]) * t, x[2] + (y[2] - x[2]) * t]);
}

function luminance(c: string): number {
  const [r, g, b] = rgb(c).map((v) => {
    const s = v / 255;
    return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
  }) as RGB;
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

export function contrast(a: string, b: string): number {
  const x = luminance(a);
  const y = luminance(b);
  return (Math.max(x, y) + 0.05) / (Math.min(x, y) + 0.05);
}

/** The more readable of two inks on a background. */
function ink(bg: string, a: string, b: string): string {
  return contrast(bg, a) >= contrast(bg, b) ? a : b;
}

/** Moves bg away from inkColor until the pair reaches 4.5:1 (or runs out of room). */
function fit(bg: string, inkColor: string): string {
  const away = luminance(inkColor) > 0.5 ? "#000000" : "#ffffff";
  let c = bg;
  for (let i = 0; i < 16 && contrast(inkColor, c) < 4.5; i++) c = mix(c, away, 0.06);
  return c;
}

// ── roles ─────────────────────────────────────────────────────────────────

/** Roles as CSS custom property name (without the leading --) to value. */
export function roles(p: Palette, mode: Mode): Record<string, string> {
  const n = p.n[mode];
  const a = p.a[mode];
  const dark = mode === "dark";
  const r: Record<string, string> = {};

  if (dark) {
    Object.assign(r, {
      "color-chrome": n[0], "color-sunken": n[0], "color-canvas": n[1], "color-panel": mix(n[0], n[1], 0.55),
      "color-overlay": mix(n[1], n[2], 0.5), "color-raised": n[2], "color-line": n[2], "color-line-strong": n[3],
      "color-fg": n[8], "color-fg-muted": n[6], "color-fg-subtle": n[5],
      "color-accent": a[5], "color-accent-hover": a[6], "color-accent-active": a[4],
      "color-accent-fill": a[3], "color-accent-fill-hover": mix(a[3], a[4], 0.4), "color-accent-fill-active": a[2],
    });
  } else {
    Object.assign(r, {
      "color-chrome": n[6], "color-sunken": n[8], "color-canvas": n[7], "color-panel": mix(n[6], n[7], 0.5),
      "color-overlay": n[8], "color-raised": mix(n[6], n[7], 0.4), "color-line": n[5], "color-line-strong": n[4],
      "color-fg": n[0], "color-fg-muted": n[1], "color-fg-subtle": mix(n[1], n[2], 0.5),
      "color-accent": a[2], "color-accent-hover": a[1], "color-accent-active": a[3],
      "color-accent-fill": a[2], "color-accent-fill-hover": a[1], "color-accent-fill-active": a[0],
    });
  }
  r["color-accent-ink"] = ink(r["color-accent"] as string, n[0], n[8]);
  // Text on the filled button stays readable in every state: pick the better ink for the fill,
  // then nudge the fill (and its hover and pressed colours) until that ink reaches 4.5:1.
  const fillInk = ink(r["color-accent-fill"] as string, a[0], a[8]);
  r["color-accent-fill-ink"] = fillInk;
  for (const k of ["color-accent-fill", "color-accent-fill-hover", "color-accent-fill-active"]) r[k] = fit(r[k] as string, fillInk);

  r["color-ok"] = dark ? "oklch(0.78 0.15 158)" : "oklch(0.5 0.13 158)";
  r["color-warn"] = dark ? "oklch(0.82 0.14 85)" : "oklch(0.52 0.12 70)";
  r["color-danger"] = dark ? "oklch(0.73 0.165 25)" : "oklch(0.52 0.18 25)";
  r["color-danger-ink"] = dark ? "oklch(0.2 0.04 25)" : "oklch(0.97 0.02 25)";

  // The stylesheet also reads the old scale directly (borders, tints). Map it onto the roles:
  // 50 is the text colour, 950 the chrome, and the steps between follow.
  Object.assign(r, {
    "color-mauve-shadow-50": r["color-fg"], "color-mauve-shadow-100": mix(r["color-fg"] as string, r["color-fg-muted"] as string, 0.5),
    "color-mauve-shadow-200": r["color-fg-muted"], "color-mauve-shadow-300": r["color-fg-subtle"],
    "color-mauve-shadow-400": r["color-accent"], "color-mauve-shadow-500": a[4],
    "color-mauve-shadow-600": r["color-accent-fill"], "color-mauve-shadow-700": r["color-line-strong"],
    "color-mauve-shadow-800": r["color-line"], "color-mauve-shadow-900": r["color-canvas"], "color-mauve-shadow-950": r["color-chrome"],
  });

  // Cards: lit from the top left, settling into the page.
  if (dark) {
    const top = mix(r["color-line"] as string, r["color-accent-fill"] as string, 0.16);
    const mid = mix(r["color-line"] as string, r["color-canvas"] as string, 0.55);
    r["card"] = `linear-gradient(155deg, ${top} 0%, ${mid} 42%, ${r["color-canvas"]} 100%)`;
    r["card-soft"] = `linear-gradient(155deg, ${mix(mid, top, 0.4)} 0%, ${mix(mid, r["color-canvas"] as string, 0.5)} 60%, ${r["color-panel"]} 100%)`;
    r["card-edge"] = r["color-line"] as string;
  } else {
    r["card"] = `linear-gradient(155deg, ${r["color-overlay"]} 0%, ${r["color-raised"]} 50%, ${r["color-canvas"]} 100%)`;
    r["card-soft"] = `linear-gradient(155deg, ${mix(r["color-overlay"] as string, r["color-raised"] as string, 0.5)} 0%, ${r["color-canvas"]} 60%, ${r["color-chrome"]} 100%)`;
    r["card-edge"] = r["color-line-strong"] as string;
  }

  for (const [k, v] of Object.entries(p.over?.[mode] ?? {})) r[k.startsWith("color-") ? k : `color-${k}`] = v;
  return r;
}

/** Four colours that stand for a palette in a mode, for its swatch. */
export function swatch(p: Palette, mode: Mode): { bg: string; panel: string; accent: string; fg: string; line: string } {
  const r = roles(p, mode);
  return { bg: r["color-canvas"] as string, panel: r["color-panel"] as string, accent: r["color-accent-fill"] as string, fg: r["color-fg"] as string, line: r["color-line-strong"] as string };
}

// ── choice and application ────────────────────────────────────────────────

const KEY_THEME = "phoenix.theme";
const KEY_MODE = "phoenix.mode";
const LEGACY: Record<string, { theme: string; mode: ModeChoice }> = {
  dark: { theme: "mauve", mode: "dark" },
  "soft-sand": { theme: "sand", mode: "light" },
  earth: { theme: "earth", mode: "light" },
  steel: { theme: "steel", mode: "light" },
};

export interface Choice {
  theme: string;
  mode: ModeChoice;
}

const media = window.matchMedia("(prefers-color-scheme: light)");
let current: Choice = { theme: "mauve", mode: "dark" };
const listeners = new Set<(c: Choice, effective: Mode) => void>();

export function effectiveMode(c: Choice = current): Mode {
  return c.mode === "system" ? (media.matches ? "light" : "dark") : c.mode;
}

export function choice(): Choice {
  return current;
}

export function onTheme(fn: (c: Choice, effective: Mode) => void): void {
  listeners.add(fn);
}

function palette(id: string): Palette {
  return PALETTES.find((p) => p.id === id) ?? (PALETTES[0] as Palette);
}

function apply(c: Choice): void {
  const mode = effectiveMode(c);
  const p = palette(c.theme);
  const root = document.documentElement;
  for (const [k, v] of Object.entries(roles(p, mode))) root.style.setProperty(`--${k}`, v);
  root.style.colorScheme = mode;
  root.dataset["theme"] = p.id;
  root.dataset["mode"] = mode;
  for (const fn of listeners) fn(c, mode);
}

export function setChoice(next: Partial<Choice>): void {
  current = { theme: palette(next.theme ?? current.theme).id, mode: next.mode ?? current.mode };
  try {
    localStorage.setItem(KEY_THEME, current.theme);
    localStorage.setItem(KEY_MODE, current.mode);
  } catch { /* the choice lasts for this session */ }
  apply(current);
}

/** Reads the saved choice (migrating the old single-name setting) and applies it. */
export function initTheme(): void {
  try {
    const t = localStorage.getItem(KEY_THEME);
    const m = localStorage.getItem(KEY_MODE);
    const legacy = t ? LEGACY[t] : undefined;
    if (legacy && !m) current = { theme: legacy.theme, mode: legacy.mode };
    else {
      if (t && PALETTES.some((p) => p.id === t)) current.theme = t;
      if (m === "dark" || m === "light" || m === "system") current.mode = m;
    }
  } catch { /* defaults */ }
  media.addEventListener("change", () => { if (current.mode === "system") apply(current); });
  apply(current);
}
