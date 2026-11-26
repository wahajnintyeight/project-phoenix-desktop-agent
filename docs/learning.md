# Learning from work

In Settings → Memory, turn on **Suggest lessons from completed work** to let the agent
propose concise preferences, experiences, and workflows after a useful correction or verified
result. Suggestions stay local in Phoenix's existing library database and require review before
they are included in later requests. This observes the agent's work, not arbitrary desktop or
browser activity. The model decides whether a task warrants a proposal, so not every task
produces one. No extra background model call, embedding service, or external memory service is
needed. Proposals require the Files tools to be available for that run. Normal model requests and
their selected context still go to the configured provider; a proposal tool call can add a model
round trip within the current request.

Review, edit, approve, give feedback on, or forget lessons in Memory settings. Approved lessons
are selected by whole-word relevance for later requests; feedback affects ordering when relevance
is equal. Marking a lesson "Needs work" returns it to review and stops it being used until it is
approved again. A lesson is context, not permission to perform an action. Phoenix should inspect the
current state before following it. Turning suggestions off stops new proposals and keeps existing
approved lessons available. Existing user memories and choice recording remain separate features.

You can assign a scope in Memory settings. A scoped lesson is recalled only when the task mentions
that scope, or its trusted task name or explicitly shared page host matches; an unscoped lesson remains available to matching tasks. A candidate can explicitly
replace an approved lesson; the old lesson remains visible as superseded and is not used.
You may pin up to two short, unscoped approved lessons so they appear in every request. Pinning
uses the same memory budget; editing a pinned lesson beyond the size limit or adding a scope
unpins it. Needs-work and superseded lessons are also unpinned.
The `learning_search` tool can look up approved lessons during a run with Files tools, even when
new suggestions are switched off. A tool call and its result use provider tokens.

Learning is bounded to 300 lessons and short fields. Memory retrieval scans the bounded local
records and includes only matching entries and a narrow set of general communication preferences.
Automatic recall includes at most three lessons. Short messages with follow-up cues may use the
last two user messages for matching; a simple greeting does not. Ordinary memories and lessons share a configurable
400–3,200-byte limit (default 1,600 bytes), excluding the learning instruction, wrapper, tool
schemas, and profile; bytes are not tokens. In a reply's details, retrieved-memory bytes refer to
the actual selected text, not a token count. Provider-reported input, output, cache and cost values describe the full request when the provider
reports them for every tool round; otherwise they are shown as not reported. Goal-run usage is
currently reported conservatively as unknown. Claude total input is the sum of reported uncached,
cache-read and cache-creation inputs; if any part is missing, the total is unknown. See
[Claude’s cache usage fields](https://platform.claude.com/docs/en/build-with-claude/prompt-caching#tracking-cache-performance).
Pinned lessons are placed in the system instructions before task-specific job guidance; other
retrieved context is sent with the current user request. Prior conversation messages and provider formats can still differ between requests, so
cache savings are not guaranteed. No provider-specific tokenizer is assumed.
It does not do semantic matching, and a proposal's evidence is an agent report, not independent
proof. Users can inspect the originating chat when it remains available. Forgetting a lesson
removes its text and keeps a bounded fingerprint to suppress an identical future proposal;
paraphrases can still be proposed. Clear memory removes facts, lessons, and those fingerprints;
it does not delete chat history or the user profile.

## Recovery

Phoenix keeps lessons and saved files together in `library.db` next to `config.yaml` (normally
`%APPDATA%\Phoenix` on Windows). Before troubleshooting a database error, fully exit all Phoenix
processes and copy `library.db` and any `library.db-wal` and `library.db-shm` files together to a
safe location. Do not delete or replace the database to clear a learning error: it also contains
other saved data. Migrations are transactional. If a newer database version is reported, open it
with a compatible newer Phoenix release; an older release will not migrate it backwards. If a
migration fails, preserve the copied files and the error message for diagnosis.

## Evaluation

`TestLearningBroaderEvaluation` adds 20 synthetic queries with a threshold fixed before its first
run: at least 8/10 direct matches and no results for five unrelated queries. On this run, both
legacy and current retrieval achieved 10/10 direct matches, 1/5 paraphrases, and 0/5 unrelated
results; no memory matched none. Across all queries legacy selected 2,176 bytes and current
2,012 bytes. This fixture was authored with knowledge of the implementation, contains no
scope or correction lifecycle cases, and is not an independent real-world validation. The
paraphrase sample is too small to justify selecting a replacement retrieval method.

`TestLearningContextScenarioEvaluation` separately checks a proposed correction before review,
then checks six cases after replacement: matching, different and absent scopes; follow-ups with
matching and different scopes; and a greeting. All six passed, and the superseded lesson was not
returned. This is a synthetic integration check authored with knowledge of the implementation,
not an independent quality estimate.

`go test ./internal/library -run TestLearningRetrievalEvaluation -v -count=1` compares synthetic
no-memory, legacy substring, current whole-word, and SQLite FTS5 retrieval when FTS5 is available.
The ten-case fixture includes eight relevant queries and two abstentions. On the development
Windows build, current retrieval returned all eight expected lessons and no flagged false hits;
legacy returned seven with one flagged false hit, and FTS5 returned eight with one flagged false
hit. The comparison uses the same header, three-result limit and byte cap for each selector; the legacy
selector therefore does not reproduce every detail of the older character-based limit. This
exploratory fixture and its threshold were refined together; they are not an independent
validation or a measure of real-world recall or the model's ability to follow a lesson. FTS5 was not adopted
because it did not improve this quality gate, and the 300-record scan took approximately 1 ms per lookup
in the capacity benchmark. Paraphrases and uncommon language remain limitations.

A separate synthetic holdout uses a threshold set before running it and formats the legacy and
current selectors identically. It includes punctuation, short acronyms, non-English text,
unrelated requests and substring traps. In the local run, the current selector found 8/8 direct
matches with 0/11 cases containing a flagged unrelated lesson; the legacy selector found 8/8 with
1/11 flagged. Across the 11 cases, the current selector returned 8 lessons and 1,358 context
bytes; the legacy selector returned 9 lessons and 1,407 bytes. Neither selector matched a
paraphrased scheduling request. This small synthetic holdout was authored with knowledge of the
implementation, so it is not an independent estimate of real-world accuracy.

At the 300-lesson capacity, `BenchmarkLearningAtCapacity` measured approximately 1 ms per local
lookup on the development machine; hardware and data shape will affect that figure. The
`TestLearningToolRequiresReviewAndHasSource` test measured 330 bytes for the MCP search schema
and 173 bytes for one sample search result. Provider-specific wrapping changes these sizes and
bytes do not establish token counts or tool-round-trip costs. Run these checks with:

```powershell
go test ./internal/library -run 'TestLearningRetrievalHoldout|TestLearningToolRequiresReviewAndHasSource' -v -count=1
go test ./internal/library -run '^$' -bench BenchmarkLearningAtCapacity -benchtime=1s -count=1
```

The optional live evaluation sends nine synthetic requests (three fixtures in each of no-memory,
legacy, and current modes), without tools. By default it uses Phoenix's configured provider,
model and vault credentials. Set `PHOENIX_EVAL_PROVIDER`, `PHOENIX_EVAL_MODEL` and
`PHOENIX_EVAL_API_KEY` to select an OpenAI-compatible HTTP provider explicitly. HTTP requests
have a 96-token output limit; CLI providers do not expose that limit through Phoenix, so their
outputs are requested to be brief but are not programmatically capped. It requires the explicit
acknowledgement below. This caps the number of calls at nine, not total dollars. Usage fields appear
only when the provider reports them; the simple answer checks are illustrative.

```powershell
$env:PHOENIX_EVAL_PROVIDER = 'openrouter'
$env:PHOENIX_EVAL_MODEL = '<model-id>'
$env:PHOENIX_EVAL_API_KEY = '<API key>'
$env:PHOENIX_LEARNING_EVAL_ACK = 'synthetic-9-calls'
go run ./cmd/learningeval
```

Optional `PHOENIX_EVAL_BASE_URL` selects an allowed OpenAI-compatible endpoint. No live call is
made by the normal test suite.

### Configured-provider run (2026-11-16)

With Phoenix configured for Codex `gpt-6-luna`, the nine-call run completed with zero tool calls.
The three no-memory checks passed for application and visa, but not resume; all six legacy and
current checks passed. These are literal substring checks against three short synthetic responses,
not a measure of general task quality. The provider reported 14,672–14,721 input tokens and 10–28
output tokens per request. Memory-bearing prompts added 184–197 bytes and about 45–49 input tokens
against the matching no-memory prompts. The provider reported 0–13,696 cached tokens across the
run; the counts varied between otherwise similar modes, so this run does not establish a cache
benefit caused by learning. Cost was not reported. The first response used 28 output tokens; the
CLI path has no enforced output-token cap, a limitation of this measurement.
