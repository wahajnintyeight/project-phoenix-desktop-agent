# Colour and theme best practices (for Phoenix)

## Accessibility is the floor, not the polish

- **WCAG AA contrast:** 4.5:1 for normal text, 3:1 for large text and for parts of controls that carry meaning (icons, input borders). `check-contrast.mjs` tests the pairs the UI really draws (text on canvas, panel and raised surfaces; accent as text; button text on every button state; danger). Passing numbers are required, not sufficient.
- **Test accent twice.** A colour that is a good button fill is often a poor text colour on the same background (a bright honey fills well and reads badly as text on linen). Phoenix therefore has separate roles: `accent` (as text or icon) and `accent-fill` plus its own ink.
- **Never carry meaning in colour alone.** Danger and success states also have words or icons; keep it that way when adding states.
- **Dark and light are separate designs, not an inversion.** Inverting a dark palette gives glaring whites and muddy accents. Light mode needs a lighter accent *darkened* for text, softer shadows, and surfaces that differ by a few percent of lightness. Dark mode needs lifted (not pure black) surfaces, and desaturated accents, because saturated colours vibrate on dark.
- **Respect "Match Windows".** Users who pick it expect the app to switch with the system, including mid-session (already implemented; do not break the `matchMedia` listener).

## Designing a palette

- **Start from the accent and one neutral family.** Tint the neutrals toward the accent's hue by a small amount (chroma around 0.01 to 0.02 in OKLCH). That is what makes surfaces feel part of the same palette. Pure greys beside a coloured accent look disconnected; heavily tinted surfaces look dirty.
- **Nine steps with even lightness.** Equal perceptual steps make the derived roles behave. If two adjacent steps are almost identical, hover and pressed states disappear; if one jumps, a border looks like a hole. Check by eye in OKLCH, not in sRGB, where steps look uneven.
- **Surface hierarchy needs only a few levels.** Phoenix uses chrome (darkest or lightest edge), canvas, panel, raised, overlay. Neighbouring levels should differ visibly but gently (about 3 to 8 points of lightness in OKLCH); lines are not needed to separate them.
- **One accent.** Use it for the primary action, the selected state and focus. If more than one part of a screen is accent-coloured, nothing is primary. Status colours (ok, warn, danger) stay readable and distinct from the accent; a danger colour near the accent hue (red accent, red danger) needs an override.
- **Keep text off the extremes.** Pure white on near-black causes halation; pure black on white feels harsh. The ramps' ends are deliberately off-white and off-black.
- **Name by role, not by value.** In CSS and components use `bg-canvas`, `text-fg-muted`, `border-line`. A literal hex in a component belongs to one theme and breaks the others.

## Process

1. Sketch the palette (accent, neutral family, mood word) and say why it exists; "another brown" is not a reason to add a palette. Every palette is something to keep working and keep checked.
2. Generate ramps, run `ramp_to_palette.py`, paste, run `check-contrast.mjs`, tune, look at it in both modes in the real app (start page, Settings, chat with tool steps and an approval card, Files), then document it.
3. When a check fails, change the cheapest thing first: a ramp value (shifts every role that reads it), then which step a role reads for that palette (different arrays per mode), then `over`. Fix the cause (a ramp too flat, a step missing) rather than patching the symptom with an override.
4. After changing `roles()`, rerun the check for **all** palettes, since one rule serves every theme.

## Keeping the set healthy

- Five palettes is already a lot to maintain; each added one doubles as two modes to test. Prefer improving an existing palette to adding a near-duplicate.
- Remove a palette only with a reason (it fails accessibility and cannot be fixed, or nobody uses it) and tell the user their selection falls back to the first palette.
- Keep `DESIGN.md` honest: palette names, what is special about each, what is not themed.
- Do not theme third-party web content, and do not add a "custom colour" picker that skips the contrast check: an unchecked colour is how unreadable UI ships.
