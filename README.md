# Kybernes

A project generator for Go services. Pick an architecture, a router, and optional extras;
Kybernes writes a plain Go project with **no dependency on Kybernes**, then helps you grow
and maintain it.

> Status: v0.1, pre-release. Targets individual teams building a monolith.

## Install

```sh
go install github.com/kellslte/kybernes/cmd/kybernes@latest   # path assumes the repo is published here
```

## Quick start

```sh
kybernes new orders --module github.com/acme/orders --arch hexagonal --router chi --db pgx --extras ci,docker,otel
cd orders && go run ./cmd/server
```

Run `kybernes new` with no name to be prompted for each choice.

## Choices

| Option | Values | Notes |
|---|---|---|
| `--arch` | `layered`, `hexagonal`, `modular` | `modular` = modular monolith |
| `--router` | `stdlib`, `chi` | handlers are plain `net/http`; the router is one file |
| `--db` | `pgx`, `mysql`, `sqlite`, `mongodb` | connection setup, `DATABASE_URL`, `compose.yaml` (not SQLite); SQL databases also get [goose](https://github.com/pressly/goose) migrations. SQLite uses a pure-Go driver (no CGO). MongoDB has no migrations. |
| `--extras` | `ci`, `docker`, `otel` | GitHub Actions, distroless Dockerfile, OpenTelemetry tracing |

## Commands

| Command | What it does |
|---|---|
| `kybernes new <name>` | Generate a project |
| `kybernes add feature <name>` | Add a feature in the project's architecture (a file per layer, or a module) |
| `kybernes add module <name>` | Alias of `add feature`, modular projects only |
| `kybernes add handler <name>` | Scaffold `internal/handler/<name>.go` |
| `kybernes add service <name> [--module m]` | Scaffold a service in the architecture's business layer (`--module` required for modular) |
| `kybernes add migration <name>` | New timestamped goose SQL file in `migrations/` (needs a SQL `--db`) |
| `kybernes migrate <up\|down\|status\|...>` | Run the embedded goose migrations against `DATABASE_URL` (needs a SQL `--db`) |
| `kybernes dev` | Build and run `cmd/server`, rebuilding and restarting on `.go`, `.sql` and `go.mod` changes; a failed build keeps the old server running |
| `kybernes doctor` | Diagnose toolchain, manifest, `go.mod` and architecture health |
| `kybernes check` | Fail if imports break the architecture's rules |
| `kybernes update [--dry-run]` | Merge newer templates into an existing project |

`add feature`, `add handler` and `add service` create the code but you wire it into `cmd/server/main.go` and `internal/handler` yourself.

Handlers, services and migrations are one-off scaffolds: they are not tracked in `kybernes.lock`,
so `update` never touches them. Migrations are embedded in the binary and also runnable directly with
`go run ./cmd/migrate up`.

### Architecture rules (`check`)

- **layered:** `repository` ← `service` ← `handler` ← `router`
- **hexagonal:** `domain` ← `app` ← `handler` ← `router`; `store` imports nothing
- **modular:** `handler` may import `modules`; a module's `internal/` is private (enforced by the Go compiler)

### How `update` treats your edits

Every generated file's hash is stored in `kybernes.lock`. On `update`: untouched files are
overwritten, files you edited are left alone, and files you edited *that also changed upstream*
get a `<file>.kybernes-new` next to them to merge by hand. Files you deleted stay deleted.
`go.mod`, `go.sum` and `kybernes.yaml` are never overwritten.

## Contributing

Templates live in `templates/` as layers (`base`, `arch/*`, `router/*`, `db/*`, `extras/*`,
`feature/*`), embedded into the binary. Files ending in `.tmpl` are rendered with `text/template`;
`__mod__` in a path becomes the feature name. Adding a router or architecture is adding a folder;
the test suite generates every combination and runs `go build`, `go vet`, `gofmt` and `kybernes check` on it.

```sh
go test ./...   # needs network (generated projects fetch dependencies)
```

Architecture contract: router layers import `internal/handler` and expect a `*Handler` with
`Health` and `Hello` methods.

## License

Apache-2.0
