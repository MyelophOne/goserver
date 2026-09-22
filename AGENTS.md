# Project instructions

- This repository develops `github.com/myelophone/goserver`, a reusable Go package and web framework. Work on the framework itself; do not apply application-template restrictions to its source code.
- Before editing, inspect `git status`, relevant source and tests, `go.mod`, and repository tasks. Preserve existing user changes and keep the diff focused on the requested work.
- Use the current implementation and README to verify APIs and behavior. Do not assume another framework's conventions or add speculative features.

## Code quality

- Follow DRY: reuse existing helpers and consolidate genuinely shared behavior instead of copying implementations. Keep abstractions small and justified by actual use; do not generalize unrelated code merely because it looks similar.
- Prefer simple, idiomatic Go, clear names, narrow interfaces, and explicit control flow. Keep functions cohesive and avoid unnecessary layers, wrappers, configuration, and dependencies.
- Fix the underlying cause in the responsible subsystem. Avoid local workarounds that leave the same defect elsewhere, unrelated refactors, and premature optimization.
- Comment only non-obvious intent, invariants, constraints, or tradeoffs. Do not narrate self-explanatory code, add change-history comments, or leave commented-out code. Document exported contracts where callers need guidance.
- Complete the requested behavior. Do not leave placeholders, fake implementations, or TODOs in place of required work.
- Follow existing error-handling conventions, preserve useful error context, and handle failures explicitly. Do not silently swallow errors or use panics for routine failures.

## Package architecture and compatibility

- Put reusable functionality in the appropriate package or subsystem. Keep `cmd/main.go` focused on assembly and demonstration, and command-specific behavior in the relevant `cmd/` tool.
- `internal/`, `web/runtime`, and `web/system` contain framework implementation and may be changed when the task requires it. Distinguish maintained source from generated output; application-template restrictions do not apply here.
- Preserve public API and observable behavior unless the requested change requires otherwise. Consider signatures, defaults, configuration precedence, HTTP behavior, middleware ordering, and downstream consumers before changing a contract.
- Keep optional subsystems optional and retain interoperability with `net/http`. Avoid introducing web tooling, database connections, or other integration requirements into unrelated package use.
- Keep development and production behavior consistent where their contracts overlap. Respect build tags and platform-specific files; check affected variants when changing shared behavior.
- Update README documentation and relevant examples when changing public APIs, configuration, or supported behavior. Make breaking changes explicit and explain migration when needed.

## Reliability and security

- Propagate contexts and cancellation; bound concurrency, retries, input sizes, caches, and outbound work. Ensure goroutines, timers, streams, listeners, and connections have clear ownership and cleanup.
- Protect shared state correctly. Avoid races, leaked goroutines, unbounded queues, and long blocking work while holding locks.
- Prevent memory and resource leaks: bound long-lived collections and caches, release references to objects no longer needed, close owned resources, stop timers, and remove event handlers and subscriptions when their lifecycle ends. When a leak is suspected, use profiling to investigate retained memory and goroutine growth.
- Preserve security defaults, validation, escaping, authentication hooks, and tenant/session isolation. Use complete cache keys for all identity, tenant, locale, and other dimensions affecting results.
- Keep secrets out of source, logs, errors, and responses. Use environment configuration and parameterized database queries; do not expose internal error details to clients.
- Support performance claims with measurements. Add benchmarks when a change targets a meaningful hot path; do not trade correctness or readability for an unverified optimization.

## GOSH and build tooling

- Treat the compiler, runtime, built-in components, generated bindings, and asset pipeline as parts of the framework. Fix behavior in its owning source rather than patching generated output.
- Preserve documented GOSH syntax and contracts, SSR/hydration behavior, layer precedence, routing, and web-disabled operation when changing the web subsystem.
- Do not hand-edit generated, compiled, cached, or tool-owned files such as `internal/goservergen/**`, `cmd/web_import_gen.go`, `tmp/goserver/build/**`, or `dist/**`. Regenerate through the repository tasks when required.
- Reuse the existing build pipeline and dependency management. Do not add a parallel toolchain, copy dependency source, modify the Go module cache, or update unrelated dependencies.

## Verify

- Format changed Go files with `gofmt`. Avoid repository-wide formatting or cleanup that introduces unrelated changes.
- Add or update focused tests for bug fixes and meaningful behavior changes. Test observable contracts, relevant edge cases, and failure paths rather than reproducing implementation details.
- Run relevant tests first; use `go test ./...`, `go vet ./...`, and `task lint` as appropriate to the scope. Use race detection for concurrency changes where supported, and check affected build tags/platforms.
- For generation, packaging, or production changes, run the relevant tasks from `taskfile.yml`, such as `task web:generate`, `task web:build`, or `task build`. Verify downstream package use when repository-local checks would miss integration issues.
- Inspect browser behavior when changing client runtime or rendered interactions. Documentation-only changes do not require application builds or test runs.
- Review the final diff for unintended behavior changes, duplicate code, unnecessary comments or dependencies, secrets, and generated artifacts. Do not repeat passing checks without a reason.
- Keep command output and responses concise. Report what changed, meaningful verification results, and any checks that could not run; do not claim unperformed validation.
