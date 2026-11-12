# Phoenix: Design

Context file for design work (see Impeccable's `DESIGN.md` convention). `PRODUCT.md` says what the product is for; this file says how it looks and why. The source of truth for every value below is `ui/app.css` (`@theme`).

## Mode and world

**Operate mode.** Phoenix is a compact desktop workspace used alongside a web page. Dark surfaces frame the page and the assistant; consistent edges, clear labels and visible selection make the interface easy to scan without adding decoration.

Calm and precise, not a developer console and not playful. Surfaces are rounded cards with a soft mauve glow from the top left, in the manner of a modern dashboard: big bold headings, pill-shaped controls and tags. Phoenix is a tool for a person who delegates work and wants to watch it, so state is always legible and nothing decorates.

## Colour

The palette is the mauve-shadow scale, defined as `--color-mauve-shadow-50` to `950` in `ui/app.css`; roles point at it.

| Token | Value | Used for |
|---|---|---|
| `chrome`, `sunken` | shadow-950 `#150e12` | Title bar, left rail, divider, inputs, the omnibox |
| `panel` | `#190f14` | Agent panel (cards sit on it) |
| `canvas` | shadow-900 `#1f141a` | Toolbar, home, settings |
| `overlay` | `#2b1d25` | Popovers, menus |
| `raised`, `line` | shadow-800 `#3d2934` | Controls, selected items, hairlines |
| `line-strong` | shadow-700 `#5c3d4e` | Input outlines, tag outlines |
| `fg` / `fg-muted` / `fg-subtle` | shadow-50 / 200 / `#b690a5` | Text, secondary text, hints and metadata |
| `--card`, `--card-soft`, `--card-edge` | gradient 155deg `#3a2631` to `#1f141a`; softer variant; `#3d2934` | Card surfaces and their 1 px edge |

**One accent, mauve.** Text, marks, dots and the caret use `accent` (shadow-400, `#ad859b`). Filled buttons use `accent-fill` (shadow-600, `#7a5268`; hover `#86596f`, pressed shadow-700) with `accent-fill-ink` (shadow-50) text. Both are for the primary action, the current selection and the running state only. `ok` (green), `warn` and `danger` (red) mean state and nothing else.

Measured contrast against the real background: smallest text is 5.1:1 (12 px metadata on the panel), placeholder 5.7:1, body text 8 to 16:1. Keep every text token at 4.5:1 or better on every surface it can sit on.

## Type

One family: Segoe UI Variable Text, falling back to Segoe UI and the system font. It is installed on every supported Windows, so no font file ships and nothing is fetched. Mono (Cascadia Mono, Consolas) is only for model IDs, paths and addresses.

Scale, about 1.15 per step: 12 / 13 / 14 / 16 / 19 / 22 px (`text-xs` to `text-2xl`). Body is 13 px in the panel and toolbar and 14 px for reading text on home and settings. Weight carries hierarchy (400 body, 600 labels and headings); there is no second typeface and no letter-spaced uppercase.

## Space, shape, depth

- Spacing follows Tailwind's 4 px unit; padding inside any bordered or filled container is at least 12 px.
- Radii: 4 (chips, tabs), 6 (controls), 8 (inputs, menus, popovers, the rail buttons), 12 (the big surfaces: home search, provider list, composer, user message).
- Cards are `rounded-xl` with a 1 px `--card-edge` and a `--card` (or quieter `--card-soft`) gradient. Use `--card` for things the user acts on (idea cards, the selected tab, approvals) and `--card-soft` for things that report (tool steps, tables, the composer).
- Pills (`rounded-full`) for buttons, tabs, tags and the segmented control; the model tag above a reply is an outlined pill.
- Inside a card, rows are separated by hairlines (inset `box-shadow`), not nested boxes.
- One shadow (`shadow-pop`), used only by popovers and menus. Depth otherwise comes from the gradient and the edge.
- No side-stripe accents, no gradient text, no glow.

## Components

- **Buttons.** Pills. Primary (deep mauve fill, light text, one per view), default (raised), quiet (text only). Small variant for dense rows. Same height as the inputs beside them.
- **Status.** A filled dot means state (ready, busy, error). A hollow ring means "not set up yet". The agent pill in the title bar shows idle or running and turns into a Stop control while a run is active.
- **Selection.** Selected rows use a `raised` background, sometimes with an accent icon or check; the active assistant tab is a pill with the `--card` gradient and edge.
- **Chat.** User messages sit in a mauve gradient bubble; agent replies are plain text with the model name in a pill above and the cost line below. A run of tool calls folds into one pill that opens into a card of rows. The empty state offers three idea cards, each with an icon tile. Errors say what happened in plain words. The Share page chip is a toggle (`aria-pressed`), off until the user turns it on, and stays as chosen.
- **Session bar.** Under the tabs, a card shows the current session (icon tile, a "Session" label, its title, a count of saved sessions) and opens a "Switch session" list: one bordered card per session with its date and message count, the open one marked Current. A bordered **New** button next to it starts a clean session (disabled until there is something to leave). Switching is disabled while a reply streams, and the list says why. Click outside or press Escape to close.
- **Unfocused controls are bordered.** Tabs, New, Share page, the model pill, Decline and Copy all carry a 1 px `line-strong` border at rest; hover and selection move the border to mauve-600.
- **Assistant panel.** A labelled header and pill tabs separate chat, activity and approvals. An 8 px divider resizes the panel by pointer or keyboard; its preferred width is remembered. Width defaults to 380 px and ranges from 300 to 840 px, with a 320 px page target when space allows. Left and right arrows adjust width by 16 px (64 px with Shift); Home and End select the available minimum and maximum, and double-click resets the default.
- **Model picker.** A fixed popover stays inside the assistant panel and groups provider, model and reasoning controls under clear labels. Providers scroll horizontally instead of wrapping into a large block; the model list uses available height up to its cap and remains independently scrollable. Provider and model radio groups support arrow keys, Home and End, and retain focus after updates. A compact selector sets reasoning effort when available, and catalogs with more than 12 models have a filter.
- **Quick actions.** A 44 px bar under the page, always visible. A small label says what Phoenix thinks the page is (a guess, so it is worded as a label, not a claim), then up to six compact buttons. The first is the likeliest request and is the only accent in the bar. Buttons disable while a reply streams. The set changes only when the page or selection changes, never under the pointer.
- **Empty states** teach the next step (the chat's empty state explains Share page; Get started lists the three things to do).
- **Popovers** use the native `popover` attribute; the list inside scrolls, the popover does not. The model picker stays within the assistant panel so the native page view cannot cover it.

## Motion

150 ms, `cubic-bezier(0.16, 1, 0.3, 1)` (ease-out-expo), on colour, opacity and transform only. `prefers-reduced-motion` removes it. Long lists use `content-visibility: auto` so streaming stays smooth.

## Accessibility

- Keyboard focus uses a visible filled surface and underlined labels rather than a border or outline.
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

## Files explorer

The Files view is a three-part explorer on the canvas: a `panel` sidebar (quick locations, kinds, folder tree, trash, usage), a main column (round nav buttons, a pill path bar, search, a ribbon of pill actions with Add files as the one filled action, the list, a status bar) and a details card on the right. Rows are 44px, selected rows take the `--card` gradient with a mauve-600 edge, folders get a stronger mauve tile, kinds are mauve-outlined pills. Drop targets (folders, tree nodes, crumbs) tint mauve-500. When the window is narrow the details card floats over the list and the Kind and Modified columns give way (container queries). Context menus use `overlay` with a 1px `line-strong` edge; Delete is the only danger-coloured action.

## Themes and modes

Seven palettes (Mauve shadow, Honey garden, Soft sand, Steel, Basic, Midnight gold, Vibrant fusion), each in dark and light, plus Match Windows. A palette is a pair of nine-step ramps (neutral and accent) in `ui/src/themes.ts`; role variables are derived from them and written once onto `<html>`. Filled-button ink is chosen and the fill nudged until it reaches 4.5:1 in every state; all 14 variants pass the contrast check. Honey garden uses honey bronze as the accent, muted teal for surfaces in dark and linen in light, and light coral for danger. Basic is plain black, charcoal and white with a monochrome accent: a near-white filled button in dark, a near-black one in light, with no hue anywhere except the status colours. Midnight gold pairs deep blue surfaces with a warm gold accent. Vibrant fusion pairs deep blue with teal and uses ember red and amber for status colours. The picker in Settings > General draws each palette in its own colours for the mode in effect. Old single-name settings (dark, soft-sand, earth, steel) are migrated on first load; the removed earth selection now opens Mauve in light mode.
