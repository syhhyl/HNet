# AGENTS.md

Repository guide for coding agents working in `HNet`.

This repo is a Go project that provides:

- `hnet`: Bubble Tea TUI client
- `hnetd`: local daemon
- `mihomo` process supervision and controller integration
- provider subscription import via `mihomo` `proxy-providers`
- macOS system proxy control via `networksetup`

No Cursor rules were found in `.cursor/rules/` or `.cursorrules`.
No Copilot rules were found in `.github/copilot-instructions.md`.

## Working Assumptions

- Primary platform is macOS.
- Go version is declared in `go.mod`.
- `mihomo` must be installed separately and available in `PATH`.
- The daemon stores runtime files under `HNET_HOME` or the user config dir.
- Root build artifacts live under `./build/` when built explicitly.

## Important Project Conventions

- Do not reintroduce custom subscription downloading/parsing when avoidable.
- The current design hands subscription URLs directly to `mihomo` through `proxy-providers`.
- Local controller calls must bypass shell proxy env vars.
- Keep phase-1 behavior minimal and operational over feature-heavy.
- Preserve the split between TUI client and daemon responsibilities.

## Build Commands

- Fast one-command build:
  - `make build`
- Default shortcut build:
  - `make`

- Build both binaries:
  - `mkdir -p build && go build -o ./build/hnet ./cmd/hnet`
  - `mkdir -p build && go build -o ./build/hnetd ./cmd/hnetd`
- Build all packages without producing root binaries:
  - `go build ./...`
- Rebuild daemon only:
  - `mkdir -p build && go build -o ./build/hnetd ./cmd/hnetd`
- Rebuild TUI only:
  - `mkdir -p build && go build -o ./build/hnet ./cmd/hnet`

## Test Commands

- Run the full test suite:
  - `make test`
  - `go test ./...`
- Run a single package:
  - `go test ./internal/config`
- Run a single test exactly:
  - `go test ./internal/config -run '^TestBuildProviderRuntimeConfig$' -v`
- Run another single test example:
  - `go test ./internal/subscription -run '^TestNormalizeURLAddsHTTPS$' -v`
- Re-run tests without cache if behavior seems stale:
  - `go test ./... -count=1`

## Lint / Format Commands

- Format all Go files:
  - `make fmt`
  - `gofmt -w ./cmd ./internal`
- Format one file:
  - `gofmt -w internal/daemon/service.go`
- Run static analysis:
  - `make vet`
  - `go vet ./...`
- Tidy dependencies after import changes:
  - `make tidy`
  - `go mod tidy`

- Run the common verification bundle:
  - `make verify`

There is no dedicated third-party linter configured yet.
Use `gofmt` and `go vet` as the baseline quality checks.

## Run Commands

- Start the daemon in background:
  - `./build/hnetd start`
- Start the daemon in foreground:
  - `./build/hnetd serve`
- Stop the daemon:
  - `./build/hnetd stop`
- Show daemon status:
  - `./build/hnetd status`
- Launch the TUI:
  - `./build/hnet`

## Sandbox / Local Testing

- Use an isolated runtime home during testing:
  - `HNET_HOME=/tmp/hnet-test ./build/hnetd start`
- This prevents polluting the main user config directory.
- Prefer `HNET_HOME` when testing subscription import or daemon restore behavior.
- Be careful with macOS system proxy tests because they affect the host machine.

## High-Level Architecture

- `cmd/hnet`: CLI entry for the TUI.
- `cmd/hnetd`: daemon entry with `serve|start|stop|status`.
- `internal/app`: user-facing app bootstrapping and TUI.
- `internal/client`: Unix socket HTTP client used by `hnet`.
- `internal/api`: shared request/response structs.
- `internal/daemon`: daemon service, route handlers, runtime orchestration.
- `internal/config`: persisted state and generated runtime config.
- `internal/mihomo`: `mihomo` process control and controller API helpers.
- `internal/platform/macos`: macOS-specific system proxy control.
- `internal/subscription`: subscription URL normalization only.

## Imports

- Use standard library imports first.
- Separate third-party imports from internal imports with blank lines.
- Keep import groups `stdlib`, third-party, then `hnet/...`.
- Let `gofmt` manage ordering.

## Formatting

- Always run `gofmt` after editing Go files.
- Keep code ASCII unless the file already contains intentional Unicode.
- Prefer short, readable functions, but do not split logic so aggressively that flow becomes harder to follow.
- Avoid needless comments; add comments only for non-obvious behavior.

## Types and Data Modeling

- Prefer concrete structs for API payloads and persisted state.
- Use `map[string]any` only when dealing with dynamic YAML/JSON structures.
- Keep JSON tags and YAML tags explicit on wire/storage structs.
- Keep daemon state in `internal/config/state.go` as the single persisted source of truth.

## Naming Conventions

- Use Go idioms: exported names in PascalCase, unexported names in camelCase.
- Keep package names short and lowercase.
- Name handlers as `handleX` and helper methods as verbs like `writeManagedConfigLocked`.
- Use `Locked` suffix only when the caller must hold a mutex.
- Use clear domain names such as `SubscriptionURL`, `ControllerPort`, `SystemProxyEnabled`.

## Error Handling

- Return errors with context using `fmt.Errorf("context: %w", err)` when wrapping.
- Use plain `errors.New(...)` for fixed user-facing validation errors.
- Prefer returning actionable daemon errors rather than generic failures.
- Persist meaningful runtime failures into daemon state when they affect status.
- Do not swallow command output from `networksetup` or `mihomo` startup failures.

## Concurrency and Locking

- `Service.mu` protects daemon state.
- Avoid doing slow I/O while holding the service mutex unless the locked helper explicitly requires it.
- If you add new locked helpers, document the expectation in the function name.
- `Supervisor` has its own mutex; do not assume service and supervisor locks are interchangeable.

## TUI Guidelines

- Keep the TUI intentionally minimal.
- Prefer keyboard-first flows.
- Show operational status clearly: running state, ports, selected proxy, system proxy state, last error.
- Avoid hiding important daemon failures behind generic UI messages.
- Preserve current Bubble Tea structure unless a larger refactor clearly improves maintainability.

## mihomo Integration Rules

- Use the local controller API for proxy selection and status.
- Do not route local controller requests through system or shell proxies.
- Keep generated config minimal; avoid importing huge provider configs into app-managed YAML.
- Prefer `proxy-providers` and small wrapper configs over app-side subscription translation.
- When changing runtime config generation, verify `mihomo` still starts and the controller becomes ready.

## macOS System Proxy Rules

- Use `networksetup`, not AppleScript.
- Assume changes are host-visible and potentially disruptive.
- Prefer enabling/disabling proxies across enabled services only.
- If adding tests around system proxy changes, ensure cleanup runs even on failure.
- Do not silently leave the machine in an altered proxy state.

## Verification Expectations

Before finishing substantive changes, try to run:

- `gofmt -w ./cmd ./internal`
- `go test ./...`
- `mkdir -p build && go build -o ./build/hnet ./cmd/hnet && go build -o ./build/hnetd ./cmd/hnetd`

For daemon/runtime changes, also try to verify one realistic flow when safe:

- start `hnetd`
- import a subscription URL
- confirm status reports `running: true`
- if applicable, confirm proxy selection via controller works

## Things To Avoid

- Do not hardcode provider-specific parsing logic if `mihomo` already supports it.
- Do not add destructive git commands.
- Do not rely on root-only flows unless the feature explicitly requires it.
- Do not mix file editing through shell redirection when normal patching is sufficient.
- Do not break the existing `HNET_HOME` override behavior.
