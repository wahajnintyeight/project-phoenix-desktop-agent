# How theming works in Phoenix (current implementation)

Read this when you change anything other than the values in a palette entry.

## The flow

1. `ui/index.html` ships `<html data-theme="mauve">`; `ui/app.css` has `@theme { … }` with Mauve's values as defaults, so the very first paint is already themed.
2. `initTheme()` (called from `ui/src/app.ts`) reads `phoenix.theme` and `phoenix.mode` from `localStorage`, migrates the old single-name setting, registers a `matchMedia` listener (used only when the mode is `system`), and calls `apply()`.
3. `apply(choice)` picks the effective mode, calls `roles(palette, mode)`, writes every result onto `document.documentElement.style` as `--<name>`, sets `color-scheme`, `data-theme` and `data-mode`, and notifies listeners (the picker redraws).
4. The stylesheet only ever reads the variables, via Tailwind utilities (`bg-canvas`, `text-fg-muted`, `border-line`…) generated from the `@theme` names. Switching a theme is one pass over about 45 properties; no CSS is swapped or re-parsed.

## A palette

```ts
{ id, name, note,
  n: { dark: Ramp, light: Ramp },   // neutral: surfaces and text
  a: { dark: Ramp, light: Ramp },   // accent
  over?: { dark?: {role: value}, light?: {...} } }  // hand-set roles; "danger" and "color-danger" are the same key
```

A `Ramp` is exactly nine hex colours, darkest first (index 0) to lightest (index 8). Dark mode normally reads the neutral ramp from its dark end for surfaces; light mode from its light end.

## Role rules (n = neutral ramp, a = accent ramp for that mode)

| Role | Dark | Light |
|---|---|---|
| `chrome`, `sunken` | n[0] | n[6] (chrome), n[8] (sunken) |
| `canvas` (page) | n[1] | n[7] |
| `panel` | mix(n[0], n[1], .55) | mix(n[6], n[7], .5) |
| `overlay` (menus, popovers) | mix(n[1], n[2], .5) | n[8] |
| `raised` | n[2] | mix(n[6], n[7], .4) |
| `line` / `line-strong` | n[2] / n[3] | n[5] / n[4] |
| `fg` / `fg-muted` / `fg-subtle` | n[8] / n[6] / n[5] | n[0] / n[1] / mix(n[1], n[2], .5) |
| `accent` (accent as text or icon) | a[5] | a[2] |
| `accent-hover` / `accent-active` | a[6] / a[4] | a[1] / a[3] |
| `accent-fill` (filled button) | a[3] | a[2] |
| `accent-fill-hover` / `-active` | mix(a[3], a[4], .4) / a[2] | a[1] / a[0] |
| `accent-ink` (text on `accent`) | the better of n[0], n[8] | same |
| `accent-fill-ink` (text on a filled button) | the better of a[0], a[8] | same |
| `ok`, `warn`, `danger`, `danger-ink` | fixed oklch values | fixed oklch values |

After the table: the three `accent-fill*` colours are nudged by `fit()` (moved toward black or white in 6% steps, at most 16) until the ink on them reaches 4.5:1. `mauve-shadow-50…950` is remapped onto roles for every palette because a lot of existing CSS reads that scale for borders and tints. `card`, `card-soft`, `card-edge` are gradients built from the roles (lit from the top left). `over` is applied last, so it wins over every rule.

## Choice, saving and migration

- `Choice = { theme: id, mode: "dark" | "light" | "system" }`. `setChoice()` saves both keys and applies at once. An unknown `theme` id resolves to `PALETTES[0]`.
- `LEGACY` maps the old single-name values (`dark`, `soft-sand`, `earth`, `steel`) to a theme and mode the first time they are seen without a `phoenix.mode`.
- `system` follows `prefers-color-scheme` live; the choice stays `system`, only the effective mode changes.
- Storage failures (private mode, quota) are swallowed: the choice then lasts for the session.

## The picker

`<ph-theme-picker>` draws a mode switch (`seg` radio group) and a grid of cards (`theme-card`, radio group with arrow-key navigation). Each card paints a small preview from `swatch(palette, mode)` (canvas, panel, accent fill, text, strong line) in the mode currently in effect, so the preview matches what selecting it will show. The picker has no palette list of its own; it iterates `PALETTES`, so adding or removing a palette needs no picker change.

## What is not themed

- Web pages in tabs (their own colours).
- The native window: `BackgroundColour` (dark mauve, shown before the page paints) and `Theme: Dark` (title bar and system menus) in `cmd/phoenix/main_windows.go`. A light theme therefore flashes a dark frame for a moment on launch. Making these follow the saved mode needs Go to learn the choice (for example the UI sends it with `send("v", …)`), which is not done today.
- `.wc-close:hover` red, on purpose: it matches Windows' own close button.
