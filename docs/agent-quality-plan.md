# Agent quality plan

This plan turns the targeted review of Eve and Vercel's agent-desktop into changes for
Phoenix. It is a roadmap, not a claim that either reference repository was reviewed file by
file.

## Phase 1: Make goal behavior repeatable

- Exercise the real Phoenix goal continuation loop with deterministic provider responses and
  synthetic tools, including no results, a real blocker, and verified completion.
- Keep the existing HTTP-provider browser evaluations and add goal-specific checks there when
  the Windows task orchestration can be driven without a real account or paid model.
- Treat a missing result as a reason to continue useful work or report blocked, never as proof
  that a search goal succeeded.

## Phase 2: Make long goals resumable

- Persist progress checkpoints, completed actions, blockers, and the latest evidence while a
  goal is running; recover unfinished work after a restart.
- Keep the existing task run history and chat session as the user-visible audit trail.

## Phase 3: Reduce wasted context and spend

- Measure prompt, tool-schema, and history token costs per goal iteration.
- Send only tools relevant to the task and trim repeated context while retaining needed evidence.
- Add a bounded per-run budget and expose actual usage in the task result.

## Phase 4: Make completion evidence-based

- Define task-specific completion checks for measurable goals such as job search.
- Require evidence for claimed outcomes and distinguish completed, blocked, and exhausted
  searches in stored task results.
- Keep human authorization as a hard boundary for applications and other external submissions.

## Phase 5: Preserve structured research state

- Track discovered, rejected, and actionable results with source, reason, and evidence so later
  iterations do not repeat the same searches or lose useful findings.
- Retain a concise user-readable summary alongside that structured state.

## Phase 6: Standardize tool outcomes

- Represent tool success, failure, denial, and retry safety explicitly, then use those outcomes
  to decide whether the goal loop should retry, recover, or stop as blocked.
- Keep desktop/browser actions verified against fresh UI state after consequential changes.

## Current implementation

Phase 1 starts by extracting and testing the goal continuation loop. The first regression covers
the observed job-hunting failure: an empty source result must not end a goal as successful.
