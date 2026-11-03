# Phoenix: Design

Context file for design work (see Impeccable's `DESIGN.md` convention). `PRODUCT.md` says what the product is for; this file says how it looks and why. The source of truth for every value below is `ui/app.css` (`@theme`).

## Mode and world

**Operate mode.** Phoenix is a tool someone sits in for hours, next to a web page that is the star. The interface should recede: quiet surfaces, one voice, and colour only when it means something. Dark first (confirmed); there is no light theme yet.

Calm and precise, not a developer console and not playful. Phoenix is a tool for a person who delegates work and wants to watch it, so state is always legible and nothing decorates.

## Colour

All colours are OKLCH. Neutrals share one hue (258, a cool blue-grey) and differ only in lightness, so depth comes from lightness steps, not shadows or borders.

| Token | L | Used for |
|---|---|---|
| `chrome` | 0.155 | Title bar, left rail |
| `sunken` | 0.165 | Inputs, the omnibox |
| `canvas` | 0.19 | Toolbar, home, settings |
| `panel` | 0.225 | Agent panel |
| `raised` | 0.255 | Controls, selected items |
| `overlay` | 0.295 | Popovers, menus |
| `line` / `line-strong` | 0.30 / 0.39 | Hairlines, input outlines |
| `fg` / `fg-muted` / `fg-subtle` | 0.945 / 0.76 / 0.64 | Text, secondary text, hints and metadata |

**One accent, ember** (`oklch(0.745 0.165 52)`), used only for the primary action, the current selection, the focus ring and the running state. If ember appears anywhere else it is a bug. `ok` (green), `warn` and `danger` (red) mean state and nothing else. Text on the accent uses `accent-ink`, never white.

Measured contrast against the real background: smallest text is 5.1:1 (12 px metadata on the panel), placeholder 5.7:1, body text 8 to 16:1. Keep every text token at 4.5:1 or better on every surface it can sit on.

## Type

One family: Segoe UI Variable Text, falling back to Segoe UI and the system font. It is installed on every supported Windows, so no font file ships and nothing is fetched. Mono (Cascadia Mono, Consolas) is only for model IDs, paths and addresses.

Scale, about 1.15 per step: 12 / 13 / 14 / 16 / 19 / 22 px (`text-xs` to `text-2xl`). Body is 13 px in the panel and toolbar and 14 px for reading text on home and settings. Weight carries hierarchy (400 body, 600 labels and headings); there is no second typeface and no letter-spaced uppercase.

## Space, shape, depth

- Spacing follows Tailwind's 4 px unit; padding inside any bordered or filled container is at least 12 px.
- Radii: 4 (chips, tabs), 6 (controls), 8 (inputs, menus, popovers, the rail buttons), 12 (the big surfaces: home search, provider list, composer, user message).
- No cards inside cards. Groups are lists separated by hairlines (inset `box-shadow`), not nested boxes.
- One shadow (`shadow-pop`), used only by popovers and menus. Everything else is flat.
- No side-stripe accents, no gradients, no glow.

## Components

- **Buttons.** Primary (ember fill, one per view), default (raised), quiet (text only). Small variant for dense rows. Same height as the inputs beside them.
- **Status.** A filled dot means state (ready, busy, error). A hollow ring means "not set up yet". The agent pill in the title bar shows idle or running and turns into a Stop control while a run is active.
- **Selection.** Selected = `raised` background plus an accent icon or check. Never a coloured stripe.
- **Chat.** User messages sit in a raised bubble; agent replies are plain text with the model name above and cost line below. Errors say what happened in plain words. The Share page chip is a toggle (`aria-pressed`), off until the user turns it on, and stays as chosen.
- **Empty states** teach the next step (the chat's empty state explains Share page; Get started lists the three things to do).
- **Popovers** use the native `popover` attribute; the list inside scrolls, the popover does not.

## Motion

150 ms, `cubic-bezier(0.16, 1, 0.3, 1)` (ease-out-expo), on colour, opacity and transform only. `prefers-reduced-motion` removes it. Long lists use `content-visibility: auto` so streaming stays smooth.

## Accessibility

- A 2 px accent focus ring on every interactive element (`:focus-visible`); never removed.
- Icons are drawn once (`ui/src/icons.ts`, 16 px grid, 1.5 px round strokes), decorative, and always paired with a text label or `aria-label`.
- The chat log does not announce every streamed word (`aria-live="off"`); the agent pill and the finished reply carry the announcement.
- Untrusted text (model output, page titles) is only ever inserted with `textContent`.

## Implementation rules

- Tailwind v4 compiled at build time (`ARCHITECTURE.md` 9e). Components are `@layer components` classes using `@apply`; markup uses semantic class names.
- The host and the UI share `id`s and `data-` attributes, never class names.
- `npx impeccable detect internal/ui/dist/index.html` (the built page) must report no findings. Source scans only run the regex rules.

## Open

- A light theme.
- A keyboard-only walkthrough, other window sizes and display scaling, and Windows 11 (snap layouts).
- A `critique` pass with screenshots by someone other than the author.
