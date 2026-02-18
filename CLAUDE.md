# Gitorum — notes for Claude

Decentralized git-backed forum. Single Go binary, local web server, no JS build step.

Module path: `github.com/gosub/gitorum`
Go version: 1.23 (net/http path parameters via `r.PathValue`)

## Build and run

    go build ./...
    go run . serve                  # starts on :8080
    go run . keygen --username alice
    go run . init --name "My Forum" --username alice

## Test

    go test ./...
    go test ./internal/crypto/... -v
    go test ./internal/forum/... -v
    go test ./internal/repo/... -v

## Package layout

| Package | Responsibility |
|---|---|
| `internal/crypto` | Ed25519 keygen, sign, verify, identity file I/O |
| `internal/forum` | Post parsing, signing, verification, Thread, Category |
| `internal/repo` | All go-git operations (init, open, commit, push/pull) |
| `internal/api` | HTTP server, JSON handlers |
| `internal/ui` | `//go:embed static` — index.html, style.css, app.js |
| `cmd` | Cobra subcommands: serve, init, keygen |

## Post file format

Front matter is TOML fenced by `+++`, not YAML.

```
+++
author    = "alice"
pubkey    = "Aa1b2c3d"          <- first 8 chars of base64 pubkey (fingerprint)
timestamp = "2026-02-17T10:00:00Z"
parent    = ""                  <- sha256 hex of parent file content; "" for root
signature = "<base64 Ed25519 sig>"
+++

Markdown body.
```

Canonical form for signing: all front matter fields except `signature`,
sorted alphabetically as `key=value\n` lines, then `\n`, then raw body.
See `crypto.CanonicalForm`.

Tombstone files: `{original_filename}.tomb` — signed by admin key only.

## Key conventions

- No external test libraries; stdlib `testing` only.
- Table-driven tests where there are multiple cases.
- Repo-package tests create a temporary bare git repo as a remote fixture.
- All git operations go through `internal/repo`; never shell out to git.
- Markdown rendered server-side (goldmark); frontend receives `body_html`.
- No CGo, no vendoring, no JS bundler or transpiler.
- Do not store binaries in the forum repo.
- Errors wrapped with `fmt.Errorf("context: %w", err)`.
- No global state; pass dependencies explicitly.
- Never add Claude as a commit co-author.

## Build steps

| Step | Status | Description |
|---|---|---|
| 1 | done | `internal/crypto` — keygen, sign/verify, identity I/O |
| 2 | done | `internal/repo` — forum init, open, commit, remote |
| 3 | done | `internal/forum` — parse, sign, verify, thread, category |
| 4 | done | `internal/api` + `internal/ui` — HTTP server, stub handlers, SPA |
| 5 | done | Wire repo into API — real reads from git working tree |
| 6 | done | Post submission — write, sign, commit, push |
| 7 | done | Sync — pull --rebase, conflict handling |
| 8 | next | Admin — tombstones, key management |
| 9 | | Setup wizard — first-run config page |
