# Repository Guidelines

## Project Structure & Module Organization
The Go backend lives under `cmd/` (entrypoints) and `internal/` (service packages) with shared types in `models/`. Database migrations, query helpers, and ACL data ship from `schema.sql`, `queries.sql`, and `permissions.json`, while static assets live in `static/` and translations in `i18n/`. The Vue 2 admin frontend sits in `frontend/`; local Docker tooling is under `dev/`, and a sample runtime config lives at `config.toml.sample`.

## Build, Test, and Development Commands
Run `make build` to compile the backend binary and `make run` for a dev server that reads assets from `frontend/dist`. `make build-frontend` emits production Vue assets, and `make dist` bundles them into the Go binary via stuffbin for releases. For frontend-only work, launch `yarn dev` inside `frontend/`, and `make dev-docker` prepares the full local stack through Docker Compose in `dev/`.

## Coding Style & Naming Conventions
Format Go code with `gofmt` (or `go fmt ./...`) before pushing; keep exports PascalCase and internal symbols lowerCamelCase to match existing packages. Follow the context-aware logging and dependency wiring patterns already present in `internal/`. Frontend code uses the Airbnb ESLint config—run `yarn lint` and name single-file components in PascalCase with matching filenames (e.g., `SubscriberList.vue`), while SCSS modules stay kebab-case.

## Testing Guidelines
Place Go tests beside source files using the `_test.go` suffix and focus on repository logic rather than external SMTP providers. Execute `make test` (alias for `go test ./...`) before opening a PR; prefer table-driven cases like those under `internal/`. Frontend end-to-end checks rely on Cypress (`npx cypress run` inside `frontend/`), so update or add specs when modifying flows such as campaign creation.

## Commit & Pull Request Guidelines
Align commit messages with the existing conventional style (`feat:`, `fix:`, `docs:`) and keep scopes tight; amend instead of stacking noisy commits. Reference GitHub issues in the footer when applicable and call out config or schema changes explicitly. PRs should include a concise summary, testing notes (`make test`, `yarn lint`), and screenshots or GIFs for UI updates in `frontend/`.

## Configuration & Security Notes
Do not commit populated `config.toml`; rely on `.sample` variants and environment variables for secrets. When developing with Docker, ensure `dev/config.toml` stays pointed at local services and avoid hard-coding API keys in migrations or seed data.
