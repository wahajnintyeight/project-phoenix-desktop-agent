---
name: phoenix-themes
description: Add, modify, rename or delete colour themes (palettes) and the dark/light/Match Windows modes in the Phoenix desktop app, and keep them accessible. Use this whenever the user mentions Phoenix themes, colour schemes, palettes, ramps, dark or light mode, the theme picker, changing accent or surface colours, importing a Coolors or Tailwind palette, removing a theme, or a colour that looks wrong, low-contrast or unreadable in some theme, even if they never say "theme". Covers the current implementation (ui/src/themes.ts), the step-by-step workflows, the contrast check, and design best practices.
---

# Phoenix themes

Phoenix's look is a set of **palettes**, each with a dark and a light mode. A palette is data: two nine-step colour ramps. Everything the stylesheet reads is derived from them by fixed rules, so most theme work is editing one array in one file and running a check. Read `references/implementation.md` before changing anything beyond a palette entry (the role rules, a new colour role, the picker or the saved setting). Read `references/best-practices.md` when designing or tuning colours.

## Where things live

| What | File |
|---|---|
| Palettes, role rules, saving and applying the choice | `ui/src/themes.ts` |
| Picker in Settings > General (cards, mode switch) | `ui/src/theme-picker.ts`, `.theme-*` and `.pv-*` rules in `ui/app.css` |
| Role names the CSS can use (`bg-canvas`, `text-fg`…) and their defaults | `@theme { … }` at the top of `ui/app.css` |
| The one remaining hard-coded colour | `.wc-close:hover` (Windows close-button red, deliberate) |
| Window background before the page paints, title-bar theme | `BackgroundColour` and `Theme` in `cmd/phoenix/main_windows.go` (fixed dark) |
| Written description | "Themes and modes" in `DESIGN.md` |

The choice is saved in the UI webview's `localStorage` (`phoenix.theme`, `phoenix.mode`), not in `config.yaml`, and Go never sees it.

## Add a palette

1. **Get two ramps**, each nine hex colours from darkest to lightest: a *neutral* one (surfaces and text) and an *accent* one. A Coolors or Tailwind export already has this order (100 darkest, 900 lightest). Turn a pasted export into an entry with:
   `python .claude/skills/phoenix-themes/scripts/ramp_to_palette.py ramps.json --id forest --name "Forest" --note "Moss and bark" --neutral bark --accent moss`
   (use `--light-neutral` / `--light-accent` when light mode should use different ramps; run it with `-h` for the rest).
2. **Paste the entry** into `PALETTES` in `ui/src/themes.ts`. The `id` is stored in users' settings, so it must be lowercase, short and never reused for a different look. Append new palettes at the end so the picker order stays stable.
3. **Run the contrast check:** `npx --yes tsx .claude/skills/phoenix-themes/scripts/check-contrast.mjs <id>`. A raw ramp usually fails a few pairs, most often the filled-button text and the accent as text in light mode. Fix by editing the ramps or adding `over` (below), then rerun until it prints `ok`.
4. **Look at it**: build and see both modes (procedure below). Numbers pass a bad design; eyes catch it.
5. **Update `DESIGN.md`** ("Themes and modes": the palette list and what is special about the new one).

Worked example (a dry run with the Honey garden ramps, one neutral ramp used for both modes): 6 of 30 pairs failed, such as `muted text on raised is 3.57:1` in dark and `accent text on canvas is 2.38:1` in light. The light failures came from reusing a mid-dark teal ramp for light surfaces; the shipped Honey garden fixes it with a separate light neutral (linen) and a deeper light accent (`--light-neutral`, `--light-accent`). Expect to split the modes when a ramp does not run from near-black to near-white.

Tuning tools, in order of preference: change the ramp step the role reads (light mode takes its accent from `a[2]`, dark from `a[5]` and `a[3]`; the table in `references/implementation.md` lists every step), give dark and light different arrays (Mauve does), then `over` for a single role (Honey sets `danger`, `danger-ink`, `ok` this way). Reach for `over` last: every override is a rule the next person has to know about.

## Modify a palette

Edit the hex values in place and rerun the contrast check for that id. Do not rename an `id` (saved settings would fall back to the first palette); change `name` and `note` freely, they are only labels. To change how every palette behaves (for example how raised surfaces are derived), edit `roles()` and run the check with no id so all of them are tested.

## Delete a palette

1. Remove its entry from `PALETTES`.
2. Anyone who had it selected falls back to `PALETTES[0]` automatically (`palette()` does this), so nothing crashes, but they are switched without being told. Say so in the change summary.
3. If it appears in the `LEGACY` map (old single-name settings: `dark`, `soft-sand`, `earth`, `steel`), repoint that entry at a palette that still exists. A legacy entry naming a missing palette also falls back to the first one, but dead entries mislead.
4. Keep at least one palette. If you remove the first one (Mauve), also change three things that assume it: the default `current` in `themes.ts`, `<html data-theme="mauve">` in `ui/index.html`, and the `@theme` defaults in `ui/app.css` (they are Mauve's values and show for a moment before the script runs). The native window colour in `cmd/phoenix/main_windows.go` (`BackgroundColour`, dark mauve) should follow the new default too.
5. Update `DESIGN.md`.

## Add a new colour role (rare)

Only when a new part of the UI needs a colour no existing role fits. Add the variable to `@theme` in `ui/app.css` (so Tailwind generates the utilities and non-JS defaults exist), add its rule to both branches of `roles()`, add a pair to `PAIRS` in `check-contrast.mjs` if it carries text, and rerun the check. Do not write a literal hex into the CSS or a component: it would ignore every theme but one.

## Change the modes

The three choices are `dark`, `light` and `system` (follows `prefers-color-scheme`, updating live). They are typed in `themes.ts` (`Mode`, `ModeChoice`) and listed in `theme-picker.ts` (`MODES`). Adding a fourth mode (high contrast, say) means a third ramp set per palette and a new branch in `roles()`; it is a design change, so propose the shape before doing it.

## Verify

1. `npx tsc --noEmit -p ui` (types), then the contrast check above.
2. Build the UI the way the app does: `npm run build:css`, `go run ./tools/buildui`, `wails3 build`. The design checker `npx --yes impeccable detect internal/ui/dist/index.html` should print `ok`.
3. Look at the result in an isolated copy of the app (never the user's running one): throwaway `APPDATA`, `LOCALAPPDATA` and `PHOENIX_TEST_PROFILE`, a `PHOENIX_DEBUG_PORT`, then drive the page over CDP. To switch themes from a script, set `localStorage["phoenix.theme"]` and `localStorage["phoenix.mode"]` and reload. Capture each mode of the new palette on the start page, Settings > General, a chat with a tool step, and the Files view. Stop the test processes and delete the throwaway folders afterwards.
4. The page inside a tab keeps the website's own colours; only Phoenix's chrome themes. Do not try to theme web pages.

## Gotchas

- `themes.ts` touches `window.matchMedia` when imported. A script that imports it outside a browser needs a stand-in (the contrast script has one).
- Role values are applied as inline styles on `<html>`, so they beat any stylesheet rule. A theme "not applying" is usually a component using a literal colour or an old token, not a bug in `apply()`.
- The old `mauve-shadow-*` scale is still read directly by a lot of CSS (borders, tints). `roles()` remaps it onto the roles for every palette; do not delete that block.
- A new palette's accent can pass as a fill and fail as text (or the reverse). The check tests both because the UI uses both.
