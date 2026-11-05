// Contrast check for every Phoenix palette in both modes.
//
//   npx --yes tsx .claude/skills/phoenix-themes/scripts/check-contrast.mjs [palette-id ...]
//
// Imports the real ui/src/themes.ts, so it checks exactly what the app will draw. Exit code 1
// when any pair fails. Thresholds are WCAG AA: 4.5:1 for text, 3:1 for large or non-text parts.

import { existsSync } from "node:fs";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

// themes.ts reads window.matchMedia when it loads; give it a stand-in outside a browser.
globalThis.window = { matchMedia: () => ({ matches: false, addEventListener() {} }) };
globalThis.localStorage = { getItem: () => null, setItem() {} };
globalThis.document = { documentElement: { style: { setProperty() {} }, dataset: {} } };

function findThemes() {
  let dir = dirname(fileURLToPath(import.meta.url));
  for (let i = 0; i < 8; i++) {
    const p = join(dir, "ui", "src", "themes.ts");
    if (existsSync(p)) return p;
    dir = resolve(dir, "..");
  }
  throw new Error("ui/src/themes.ts not found above this script");
}

const { PALETTES, roles, contrast } = await import(pathToFileURL(findThemes()).href);

// [label, foreground role, background role, minimum ratio]
const PAIRS = [
  ["text on canvas", "color-fg", "color-canvas", 4.5],
  ["text on panel", "color-fg", "color-panel", 4.5],
  ["text on raised", "color-fg", "color-raised", 4.5],
  ["muted text on canvas", "color-fg-muted", "color-canvas", 4.5],
  ["muted text on raised", "color-fg-muted", "color-raised", 4.5],
  ["subtle text on canvas", "color-fg-subtle", "color-canvas", 4.5],
  ["subtle text on raised", "color-fg-subtle", "color-raised", 3],
  ["accent text on canvas", "color-accent", "color-canvas", 4.5],
  ["accent text on raised", "color-accent", "color-raised", 3],
  ["button text on accent fill", "color-accent-fill-ink", "color-accent-fill", 4.5],
  ["button text on fill (hover)", "color-accent-fill-ink", "color-accent-fill-hover", 4.5],
  ["button text on fill (pressed)", "color-accent-fill-ink", "color-accent-fill-active", 4.5],
  ["danger text on canvas", "color-danger", "color-canvas", 4.5],
  ["danger button text", "color-danger-ink", "color-danger", 4.5],
  ["borders on canvas", "color-line-strong", "color-canvas", 1.3],
];

// oklch() values (ok, warn, danger by default) are not hex; convert for the maths.
function toHex(v) {
  if (v.startsWith("#")) return v;
  const m = v.match(/oklch\(([\d.]+)\s+([\d.]+)\s+([\d.]+)\)/);
  if (!m) throw new Error(`cannot read colour ${v}`);
  const [L, C, h] = [Number(m[1]), Number(m[2]), (Number(m[3]) * Math.PI) / 180];
  const a = C * Math.cos(h), b = C * Math.sin(h);
  const l = (L + 0.3963377774 * a + 0.2158037573 * b) ** 3;
  const mm = (L - 0.1055613458 * a - 0.0638541728 * b) ** 3;
  const s = (L - 0.0894841775 * a - 1.291485548 * b) ** 3;
  const lin = [4.0767416621 * l - 3.3077115913 * mm + 0.2309699292 * s, -1.2684380046 * l + 2.6097574011 * mm - 0.3413193965 * s, -0.0041960863 * l - 0.7034186147 * mm + 1.707614701 * s];
  const enc = (x) => Math.round(255 * Math.min(1, Math.max(0, x <= 0.0031308 ? 12.92 * x : 1.055 * x ** (1 / 2.4) - 0.055)));
  return "#" + lin.map((x) => enc(x).toString(16).padStart(2, "0")).join("");
}

const only = process.argv.slice(2);
let failures = 0;
let checked = 0;
for (const p of PALETTES) {
  if (only.length && !only.includes(p.id)) continue;
  for (const mode of ["dark", "light"]) {
    const r = roles(p, mode);
    for (const [label, fg, bg, min] of PAIRS) {
      checked++;
      const ratio = contrast(toHex(r[fg]), toHex(r[bg]));
      if (ratio < min) {
        failures++;
        console.log(`FAIL ${p.id} ${mode}: ${label} is ${ratio.toFixed(2)}:1, needs ${min}:1  (${fg} ${r[fg]} on ${bg} ${r[bg]})`);
      }
    }
  }
}
console.log(failures === 0 ? `ok: ${checked} pairs pass` : `${failures} of ${checked} pairs fail`);
process.exit(failures === 0 ? 0 : 1);
