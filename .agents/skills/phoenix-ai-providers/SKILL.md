---
name: phoenix-ai-providers
description: Add, update, or debug AI providers in the Phoenix desktop agent. Use whenever a task adds a model vendor, changes provider authentication, model discovery, streaming, tool calls, usage reporting, provider settings UI, or provider tests in this repository.
---

# Phoenix AI Providers

Use this skill for provider work in this repository. Follow the existing provider abstraction and keep credentials inside the existing vault/CLI flows.

## First, trace the current flow

Read the relevant implementation before editing:

- `internal/provider/provider.go`: shared types, `Provider` contract, events, requests, results, model metadata, validation.
- `internal/provider/`: vendor adapters. CLI integrations include `claude.go` and `codex.go`; HTTP integrations use the shared OpenAI-compatible implementation and its vendor configuration.
- `internal/providerhub/hub.go`: provider registration, settings resolution, secret vault, model-list caching, runs, cancellation and MCP tool lifecycle.
- `internal/config/`: provider IDs, defaults, auth mode, model, effort, CLI path and base URL persistence.
- `cmd/phoenix/providers_windows.go`: Go-to-UI settings protocol, status/model refresh, actions and user-facing errors.
- `ui/src/provider-card.ts` and `ui/src/model-list.ts`: provider settings and model selection.
- Nearby tests in `internal/provider`, `internal/providerhub`, and `cmd/phoenix`.

Search for `provider.AllIDs`, `NewHTTPProviders`, `IsHTTP`, and protocol action names to find every registration and UI dependency. Keep source files below 1,000 lines; split by responsibility if needed.

## Choose the matching integration

- Prefer the shared HTTP provider implementation when the vendor exposes an OpenAI-compatible chat completions API. Add vendor-specific configuration there rather than cloning the request/streaming/tool loop.
- Add a dedicated `provider.Provider` implementation when the vendor requires a distinct protocol or CLI. Follow the closest existing adapter and keep process management in the provider package.
- Preserve the current credential boundary: never read vendor credential files directly, put API keys only in the encrypted vault, and never include secrets in status JSON, logs, errors, or UI messages.
- Keep provider IDs stable because they key persisted configuration, vault entries, UI element IDs, and defaults.

## Implement the complete provider path

For a new provider, trace and update only the relevant pieces of this path:

1. Define its stable ID, display name, authentication mode, and defaults.
2. Register it in the provider registry/factory and display ordering. Confirm `providerhub.NewWithStore` includes it.
3. Resolve its settings through `providerhub`; use the existing vault reference flow for API keys. Validate base URLs and model/effort inputs at the boundary.
4. Implement status detection, model listing, and run behavior. Return normalized `provider.Event` values and `provider.Result`; propagate context cancellation and report vendor errors without secrets.
5. For HTTP adapters, preserve bounded request/response handling, timeouts, SSE termination/error behavior, and the existing tool-call approval rules. Do not advertise tools the adapter cannot execute.
6. Connect settings controls and protocol actions in `cmd/phoenix/providers_windows.go` and `ui/src/provider-card.ts` only when the provider needs distinct UI. Reuse the generic card/model UI when possible.
7. Add focused unit tests for registration, settings/auth validation, status/model parsing, streaming, errors and cancellation. Add hub tests for config/vault integration and secret redaction where applicable.

For updates to an existing provider, identify the changed contract first (auth, model IDs, streaming, tools, limits, or usage) and preserve compatibility with saved config and existing model selections.

## Verify

- Run focused tests for the changed provider and provider hub; then run the repository’s normal Go test/build commands if practical.
- Check formatting with `gofmt` for changed Go files and inspect the final diff for accidental credential logging or protocol changes.
- Do not claim live vendor compatibility from mocks alone. If no real vendor request was authorized or available, say that integration behavior remains unverified.
- Keep tests deterministic: use fake executables, `httptest` servers, or existing test seams. Never require a real API key or contact a paid endpoint in tests.

## Done means

The provider is discoverable by the app, its settings and secrets follow existing Phoenix rules, its responses map to the shared event/result contract, errors and cancellation behave consistently, and focused regression tests cover the changed behavior.
