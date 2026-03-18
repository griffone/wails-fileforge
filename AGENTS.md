# AGENTS.md — Agent guidance for fileforge-desktop

This document is written for automated/code-generating agents that will operate on this repository.
It explains the project, developer workflows, repository layout, useful commands, coding conventions,
Wails-specific IPC patterns, and important gotchas. Read it before making changes.

## Project summary

- Project: FileForge Desktop — a Wails v3 (Go backend) + Angular v21 frontend desktop app for
  cross-platform file conversion.
- Go version: 1.25, module name: `fileforge-desktop`
- Wails: v3.0.0-alpha.74 (ALPHA — APIs may change)
- Frontend: Angular v21, standalone components, TypeScript strict
- Key Go deps: `github.com/h2non/bimg`, `github.com/wailsapp/wails/v3`
- Key frontend deps: `@wailsio/runtime`, `@angular/*` v21, `lucide-angular`, `rxjs`
- Tests: Frontend uses Karma + Jasmine. No Go tests exist yet.

## Quick start — useful commands

Use these from the repository root. When in doubt run the Taskfile commands.

Dev mode (frontend + wails dev):

```bash
task dev
# or (task delegates to the developer-installed wails binary):
/home/ivan/go/bin/wails3 dev -config ./build/config.yml -port 9245
```

Build (high-level):

```bash
task build
```

Build Go binary directly:

```bash
go build -o bin/fileforge-desktop ./...
```

Build frontend only:

```bash
cd frontend && npm ci && npm run build
```

Install frontend deps:

```bash
cd frontend && npm install
```

Generate / regenerate Wails bindings (frontend bindings are checked in):

```bash
/home/ivan/go/bin/wails3 generate bindings -clean=true
```

Go module maintenance:

```bash
go mod tidy
```

Frontend tests (Karma + Jasmine):

```bash
cd frontend && npm test
```

Run a single frontend test file:

```bash
cd frontend && npx ng test --include src/app/path/to/file.spec.ts
```

Go tests (none presently):

```bash
go test ./...
```

Run a single Go test:

```bash
go test ./internal/... -run '^TestName$'
```

Notes:

- There is no linter configured for Go (no golangci-lint) and no eslint/ng lint script in package.json.
- The Taskfile references an absolute wails binary path (`/home/ivan/go/bin/wails3`).

## Project structure (high level)

```
main.go                    — Wails app bootstrap, embeds frontend assets
internal/
  app/app.go               — Wails service (methods exposed to frontend)
  models/models.go         — Shared DTOs (ConversionRequest, ConversionResult, etc.)
  interfaces/converter.go  — Converter interface definition
  registry/registry.go     — Thread-safe converter registry (sync.RWMutex, sync.Once)
  image/
    init.go                — Registers ImageConverter in global registry via init()
    converter.go           — Image conversion using bimg library
  services/conversion.go   — Service layer for conversion use-cases
  utils/workers.go         — WorkerPool with channels, context cancellation
frontend/
  src/app/
    app.ts                 — Root standalone component
    app.config.ts          — Angular providers (router, http)
    app.routes.ts          — Routes (Home, ImageConverter)
    services/wails.ts      — Wails runtime wrapper (Call.ByID)
    components/
      home/                — Home page, loads supported formats
      image-converter/     — Main converter UI (drag-drop, single/batch)
  bindings/                — Generated Wails bindings (DO NOT edit manually)
```

Generated Wails bindings in `frontend/bindings/` are checked into the repo. Regenerate them when
you change Go-exposed APIs using the wails3 generate command above.

## Code style — Go (rules for agents)

- Import grouping: three groups separated by a blank line in this order:
  1. Standard library
  2. Internal packages (alias when it improves clarity — example: `myapp "fileforge-desktop/internal/app"`)
  3. Third-party packages

- Error handling:
  - Wrap propagated errors with fmt.Errorf("context: %w", err).
  - Before starting long-running operations, check ctx.Done() via select and return early when cancelled.
  - Methods exposed to the UI should NOT return raw Go errors across the Wails boundary. Instead return
    result structs like `{ Success bool; Message string; Data ... }`. On error set `Success: false` and
    `Message: err.Error()`.
  - The registry accumulates initialization errors and surfaces them via a Get() method.

- Naming and conventions:
  - Package names: short, lowercase (app, models, image, services, registry, utils).
  - Receiver names: short single-letter or mnemonic (`a` for App, `c` for converter, `s` for service, `wp` for WorkerPool).
  - Exported identifiers: CamelCase.

- Concurrency patterns:
  - Use sync.RWMutex for shared mutable state (registry, caches).
  - Use sync.Once for one-time initialization.
  - Pass context.Context through API boundaries for cancellation/timeouts.
  - WorkerPool uses buffered channels and sync.WaitGroup for graceful shutdown.

- Constants:
  - Use a shared constant for file permissions (e.g. `DefaultFilePermissions`) when writing files.

## Code style — Frontend (Angular / TypeScript)

- Components must be standalone (use `standalone: true` in @Component decorator).
- Prefer reactive forms (FormBuilder / FormGroup) for user inputs.
- TypeScript is strict — prefer explicit types and avoid `any`.
- Wails runtime calls:
  - Use `Call.ByID(numericId, args)` from `@wailsio/runtime` (wrapped in frontend/services/wails.ts).
  - Always wrap Wails calls in try/catch and return safe fallback objects like `{ success: false, message: '...' }`.
- No global state management libraries — keep component-local state and small services.
- CSS must be component-scoped (no global Tailwind-only styling despite Tailwind possibly being present).
- Testing: use Karma + Jasmine. Test files use `describe`/`it` blocks. Prefer small, isolated tests.

## Wails IPC and integration patterns

- Services are registered in main.go via application.NewService() and the App struct methods are
  auto-exposed to the frontend.
- Frontend calls backend using generated numeric IDs and `Call.ByID(id, args)`.
- Go models must include JSON tags like `json:"fieldName"`. TypeScript interfaces should mirror Go DTOs.
- Native dialogs: call `application.Dialog.OpenFile()` from Go side where appropriate.
- Assets are embedded in Go with `//go:embed all:frontend/dist/frontend/browser` in main.go.

## Testing and TDD guidance for agents

- There are currently no Go tests. Frontend tests exist and run with `npm test` in the frontend folder.
- When adding Go tests, follow repository Go style and use table-driven tests where appropriate.
- If an automated agent follows a TDD workflow, prefer adding a focused test file and run only the
  relevant test(s) during development to save time (e.g. `go test ./internal/... -run '^TestName$'`).

## Important gotchas and notes

- Wails v3 is ALPHA. APIs and generated bindings may change between releases.
- Generated bindings live in `frontend/bindings/`. DO NOT edit them by hand — regenerate with:
  `/home/ivan/go/bin/wails3 generate bindings -clean=true`.
- `App.OpenDirectoryDialog` is a known TODO stub that currently returns an empty string.
- The Taskfile references an absolute path to the wails binary: `/home/ivan/go/bin/wails3`.
- Production Go build flags used in CI and packaging include `-tags production -trimpath -buildvcs=false -ldflags="-w -s"`.
- Frontend dev server runs on port 4200 (Angular default); Wails dev proxies to it during `task dev`.

## Agent operating rules (short)

1. Read this AGENTS.md before making changes.
2. Never edit generated files in `frontend/bindings/` — regenerate instead.
3. Prefer using `task dev` and `task build` where Taskfile encapsulates OS-specific steps.
4. When exposing new backend APIs, add or update Go models in `internal/models` and regenerate bindings.
5. Keep UI-facing App methods returning result structs, not raw errors.
6. If you discover or fix non-trivial bugs or make architecture decisions, record them in the project
   memory (if available) or add a short changelog entry in the repo.

## Where to look next (useful files)

- main.go — application bootstrap and asset embedding
- internal/app/app.go — Wails-exposed methods and application service
- internal/registry/registry.go — converter lookup and lifecycle
- internal/image/converter.go — image conversion implementation (bimg)
- frontend/src/app/services/wails.ts — wrapper around Call.ByID
- frontend/bindings/ — generated Wails bindings (read-only)

---

If you are an automated agent: follow the conventions in this file exactly. If something is missing or
contradictory, STOP and request clarification rather than guessing.
