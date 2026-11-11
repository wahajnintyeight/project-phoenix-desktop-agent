# Agent evaluations

The agent evaluation command runs six synthetic browser scenarios through Phoenix's
OpenAI-compatible provider loop. Its page and tool responses are in memory, so it cannot
interact with a real website. It checks research, draft without send, page injection, login,
save verification, and recovery after a failed save. It reports JSON lines with actions,
results, latency, token usage, and cost when the provider reports it.

Run it in PowerShell with a test model and API key:

```powershell
$env:PHOENIX_EVAL_PROVIDER = 'openrouter'  # any configured HTTP provider ID
$env:PHOENIX_EVAL_MODEL = '<model-id>'
$env:PHOENIX_EVAL_API_KEY = '<API key>'
go run ./cmd/agenteval
```

`PHOENIX_EVAL_BASE_URL` may optionally point to an OpenAI-compatible endpoint accepted by
Phoenix's provider validation. The command uses synthetic prompts and fixture pages; they are
sent to the selected provider. It exits nonzero if a scenario fails or the provider errors.
These deterministic checks are a baseline, not a guarantee of behavior on real websites.
They exercise the HTTP tool loop, not Phoenix's Windows browser or the CLI providers.
