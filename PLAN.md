# Learning mode: implementation plan

## Goal

Make approved knowledge useful across tasks without growing each model request with the size of the knowledge base. Keep storage and retrieval local, use Go and the existing SQLite database, preserve user review, and measure both task quality and provider-reported token usage. A retrieved lesson is untrusted context, never permission to act.

## Current state

- Learning suggestions are opt-in; candidates remain local and require user review. Approved, active lessons are the only lessons included in automatic or on-demand recall.
- SQLite schema version 5 migrates transactionally. Scope, supersession, pinning, and concurrent capacity checks are implemented. The Memory UI supports review, editing, replacement, pinning, feedback, and forgetting.
- Retrieval scans up to 300 records locally, uses bounded whole-word matching and trusted task/site scopes, returns up to three lessons, and can abstain. It is lexical and may miss paraphrases.
- Ordinary memories, dynamic lessons, and pinned lessons share a configurable 400–3,200-byte budget. The byte count is not an exact token count; pinned guidance is capped at two short lessons.
- Stable browser policy and pinned preferences precede task-specific browser guidance. Dynamic context is attached to the current user message. Provider request ordering and usage reporting have regression tests; Claude input totals include cache read/write counts only when all fields are reported.
- FTS5 was available in the development SQLite build but was not adopted: the synthetic comparison had a flagged unrelated match. Full Go tests, UI typecheck/build, review-control interaction checks, and race checks passed during implementation.
- The optional live evaluator ran nine synthetic requests through configured Codex `gpt-6-luna`, with zero tools. The literal checks passed 2/3 without memory and 6/6 across the two memory modes (8/9 overall); these tiny fixtures do not establish general quality. Provider input was 14,672–14,721 tokens and output 10–28 tokens per request; cost was not reported. Cached-token counts varied across modes and do not demonstrate learning-driven cache savings. CLI outputs have no enforced token cap. Full details are in `docs/learning.md`.

## Phase 1 — Baseline and evaluation

1. Add a deterministic, synthetic fixture set for job searches, visa and location constraints, resume corrections, file workflows, short follow-ups, unrelated tasks, contradictory and forgotten lessons, and multilingual or long input. Define expected relevant lessons, forbidden matches, and cases where retrieval must abstain.
2. Evaluate current retrieval and a no-memory control. Report recall, irrelevant matches, abstention, count and bytes of selected lessons, and retrieval latency. Set explicit acceptance thresholds from the baseline before tuning; keep private user data out of committed fixtures.
3. Add an opt-in live evaluation using synthetic scenarios and a user-supplied provider configuration. Record task success, provider-reported input/output/cached tokens and tool calls, duration, and reported cost where available. Set an explicit run budget. Report unavailable usage as unknown, not zero. Run no paid calls automatically.

## Phase 2 — Safe knowledge lifecycle

1. Add an actual transactional migration path for `internal/library/store.go`: read and validate `user_version`, stop resetting it on every open, migrate and backfill existing databases, and preserve lessons, ordinary memories, settings, and suppression fingerprints. Fail safely on unsupported versions or migration errors.
2. Extend lessons minimally with scope and supersession metadata. Scope must come from trusted task/site identifiers or explicit user input; unknown or legacy scope must remain usable without inventing a site. Keep source chat and evidence, marked as model-reported.
3. Show likely duplicates or conflicts for user review. Approval of a correction atomically supersedes the selected old lesson; proposals never promote or overwrite approved content automatically. Deleted and superseded lessons cannot be retrieved. Preserve pause and clear semantics.
4. Enforce storage limits atomically across connections; cover concurrent proposal/review and migration failures. Never expose sensitive content in diagnostic logs.

## Phase 3 — Bounded local retrieval

1. Implement deterministic whole-token ranking and scope checks in Go, including short follow-up context and a relevance threshold that can return no lessons. Rank only active, approved, non-superseded lessons. Keep the bounded 300-record scan initially; benchmark quality and latency before adding an index.
2. Probe whether the existing Windows `modernc.org/sqlite` build supports FTS5. Compare FTS5/BM25 with the bounded Go ranking against the same fixture set; adopt it only if it improves the agreed quality or latency thresholds. Handle indexing, migration, deletion, and rebuild consistently if selected.
3. Start with at most three lessons and an adjustable target of 200–400 tokens for learning context. Implement verified provider token counting where available; elsewhere apply a conservative byte cap and identify token numbers as estimates. Bound ordinary memory and lessons together, while retaining explicitly pinned critical preferences. Character or byte limits must not be presented as exact token guarantees.
4. Compare against Phase 1 and keep the change only when it meets the predefined quality and budget gates. Test false positives, synonyms and paraphrases as known lexical limitations, no-match cases, punctuation, non-English text, and oversized input.

## Phase 4 — Prompt and cache integration

1. Refactor `cmd/phoenix/memory_windows.go` and `cmd/phoenix/chat_windows.go` so stable policy and explicitly pinned global preferences precede task-specific context wherever each provider supports it. Keep message ordering, trust boundaries, chat history, and existing provider behavior intact.
2. Review `internal/provider/{openai,claude,codex}.go` independently. Verify actual request payloads and cache behavior; do not infer cache hits from a stable prefix or `CacheSession`. Track full-request usage separately from estimated memory contribution, including tool schemas and any tool round trips. Avoid logging prompts or lesson content.
3. Report actual input, cached and output tokens and cost only when the provider supplies them. Compare no memory, current memory, and new retrieval on the same synthetic scenarios. Do not claim a cache improvement until measured.

## Phase 5 — Recall and user controls

1. Add a bounded on-demand memory-search tool through the existing library MCP server. Return concise approved results, capped by count and size, with identifiers; include scope and provenance where useful. Search is local and uses no background LLM call. Its tool schema, request, and response still consume tokens and must be measured.
2. Extend `ui/src/memory-settings.ts` and the existing Go bridge with clear review controls for scope, source, conflict/supersession, and deletion. Explain that evidence is model-reported and learning can miss corrections. Surface selection reason and measured/estimated usage only where the implementation can support them accurately.
3. Update `docs/learning.md` with privacy, budgets, provider limitations, opt-in behavior, and operational recovery. Do not change how ordinary memories or saved chats are deleted without an explicit user action.

## Verification and release gates

- Migration tests: fresh database, representative older databases, reopen, rollback on failure, existing data preservation, and unsupported future versions.
- Learning tests: candidate isolation, approval, correction, concurrent updates, pause, forget, suppression, clear, and scoped retrieval.
- Request tests: deterministic budget enforcement, stable ordering, no unexpected model call, provider payloads and missing usage reporting. Integration tests cover proposal → review → later request and on-demand recall.
- Run the synthetic benchmark with and without memory, then optional live scenarios with consent and budget. Publish accuracy, added input/cached/output tokens, latency, and known gaps; do not claim general reliability from unit tests alone.
- Run relevant Go tests, UI typecheck and UI interaction checks. Keep every changed file at or below 1,000 lines; split by responsibility if needed.

## Deferred until evidence warrants it

Semantic embeddings, a background model, graph memory, automatic consolidation, and external services. Reconsider semantic retrieval if the evaluation shows material paraphrase misses, and measure its resource, privacy, and token costs first.

## Implementation verification notes

The local implementation, migration and review controls are in place. `go test ./...`, UI
typechecking and the rendered review-control interaction check pass. `go test -race
./internal/library ./cmd/phoenix ./internal/provider` also passes. The synthetic comparison,
capacity benchmark and opt-in provider evaluation are described in `docs/learning.md`.

The original exploratory fixture and its acceptance threshold were refined during implementation,
so they cannot retrospectively establish a pre-tuning gate. A separate holdout has a threshold
fixed before its run, but was written with knowledge of the implementation and is not a
real-world validation. A live provider run is now recorded in `docs/learning.md`; it is a three-scenario smoke check,
not a quality benchmark. No cache improvement is claimed. Byte limits are not verified token
counts, and retrieval remains lexical.
