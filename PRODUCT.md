# Phoenix: Product

Context file for design work (see Impeccable's `PRODUCT.md` convention). Items marked **Assumption** have not been confirmed by the owner.

## Purpose

Phoenix is a Windows desktop workspace where a person and an AI agent work together in a browser and a file manager. First use case: researching jobs, tailoring résumés, organizing documents and preparing applications.

## Users

- A single person on a personal Windows PC who delegates repetitive web and document work to an agent and wants to stay in control.
- Comfortable with technology; not necessarily a developer.

## What the interface must do

- Show what the agent is doing at all times, with a Stop control that is always reachable.
- Carry out the user’s requested workflow autonomously and keep actions and progress visible.
- Feel immediate: clicks respond within a frame; nothing blocks on the agent.
- Keep the user's data under their control: visible, configurable storage locations; no surprises about what the agent can access.

## Experience principles

1. **Dark first.** Dark theme by default (confirmed).
2. **The page is the star.** Chrome stays out of the way; the agent panel is collapsible.
3. **Status over decoration.** State (idle, running, waiting, error) is always legible at a glance.
4. **Calm and precise.** **Assumption:** quiet, restrained, well-spaced; not a dense developer console and not playful.
5. **Honest defaults.** Make enabled capabilities and autonomous operation clear and easy to stop.

## Layout decisions (confirmed)

Borderless window with a custom title bar; tabs in the title bar; agent panel on the right; left rail for Browser, Files, Tasks. Closing the window keeps Phoenix running in the system tray.

## Non-goals (for now)

- Light theme and portable mode (later).
- Mobile or cross-platform builds.
- Bulk scraping or anything that violates the terms of the sites the user uses.

## Anti-patterns to avoid (from Impeccable's list)

One font for everything; purple-to-blue gradients; cards nested in cards; gray text on colored backgrounds; an icon tile above every heading; cramped padding; skipped heading levels; low contrast; tiny text.

## Open questions

- Confirm tone (calm and precise vs denser and technical).
- Brand name treatment, logo and accent color.
- Whether `DESIGN.md` should be drafted before or alongside the first component prototype.
