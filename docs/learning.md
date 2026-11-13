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
are selected by keyword relevance for later requests; feedback affects ordering when relevance
is equal. Marking a lesson "Needs work" returns it to review and stops it being used until it is
approved again. A lesson is context, not permission to perform an action. Phoenix should inspect the
current state before following it. Turning suggestions off stops new proposals and keeps existing
approved lessons available. Existing user memories and choice recording remain separate features.

Learning is bounded to 300 lessons and short fields. Memory retrieval scans the bounded local
records and includes only matching entries and a narrow set of general communication preferences.
It does not do semantic matching, and a proposal's evidence is an agent report, not independent
proof. Users can inspect the originating chat when it remains available. Forgetting a lesson
removes its text and keeps a bounded fingerprint to suppress an identical future proposal;
paraphrases can still be proposed. Clear memory removes facts, lessons, and those fingerprints;
it does not delete chat history or the user profile.
