# Contributing to NodePanel

NodePanel is the control plane for selling node access: it manages a fleet of
[NodeAgent](https://github.com/loopy-iri/NodeAgent) nodes, customers, plans and
subscriptions, and exposes usage to an external sales bot. Thanks for helping!

> Repo: https://github.com/loopy-iri/NodePanel · Node: https://github.com/loopy-iri/NodeAgent
> Full guide: the [`docs/wiki/`](docs/wiki/Home.md) folder (also published as the Wiki).

## Scope (important)
The panel has **no money logic**. Wallet, pricing and payments live in the
external sales bot. The panel only manages bytes/time/status and reports
usage/overage via the API and signed webhooks. Please keep PRs within this scope.

## Reporting issues
Open a GitHub issue and include:
- What you expected vs what happened.
- Logs: `sudo pg-panel logs` (or `journalctl -u pg-panel`).
- Repro steps and the API request/response if relevant (censor the Bearer token).

Never paste the panel API token, node master keys or customer keys.

## Development setup
- Go (see `go.mod`).
- Build and test before a PR:
  ```bash
  go build ./...
  go test ./internal/...
  go vet ./...
  gofmt -l .          # must print nothing
  ```
- Run locally:
  ```bash
  PANEL_API_TOKEN=local-dev-token-please-change-me go run ./cmd/panel
  # UI:   http://localhost:8080/
  # Docs: http://localhost:8080/docs
  ```

## Pull requests
- Branch off `main`; keep PRs focused.
- When you add or change an endpoint, update **all** of:
  - the handler + store,
  - `internal/web/assets/openapi.yaml` (and the schema),
  - the web UI (`internal/web/assets/app.js`) if user-facing,
  - `docs/wiki/` (Persian page + its `-EN` English counterpart) and the README.
- The web UI is embedded in the binary; after changing assets, rebuild to test.
- Don't change the Go module path (`github.com/pasarguard/panel`).
- Use parameterized SQL (the store already does) and never log secrets.
- Releases are built by `release.yml` on a `v*` tag.

## Project structure
```
cmd/panel/            entrypoint
internal/store/       SQLite + schema + migrations
internal/nodeclient/  node client (master key + cert pin/TOFU)
internal/api/         REST API + usage collector + lifecycle
internal/webhook/     HMAC-signed dispatcher
internal/web/         embedded web panel + openapi.yaml + Swagger UI
scripts/pg-panel.sh   installer/manager (prebuilt binary + systemd)
docs/wiki/            bilingual (FA/EN) documentation
```
