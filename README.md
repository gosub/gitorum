# Gitorum

A decentralized forum where all content is stored as signed files in a git
repository and distributed via standard git transports (HTTP, SSH, email
patches).

## How it works

The binary starts a local HTTP server that serves a single-page web UI and a
JSON API. All forum data lives in a local git repository managed by the binary
using go-git. Users interact through the browser; git operations happen
transparently in the background.

Every post is an Ed25519-signed Markdown file committed to the repository.
Syncing with other participants is plain `git push` / `git pull --rebase`.
There is no central server, no database, and no account system beyond a keypair
stored in `~/.config/gitorum/identity.toml`.

## Repository layout

The forum data format is a plain directory tree inside a git repository:

```
/
├── GITORUM.toml                    forum name, description, admin public key
├── keys/
│   └── {username}.pub              each user's Ed25519 public key (base64)
└── {category}/
    ├── META.toml                   category name and description
    └── {thread-slug}/
        ├── 0000_root.md            original post
        └── {timestamp}_{hash8}.md replies, e.g. 1708123456789_a3f9c1b2.md
```

Each `.md` file has a TOML front matter block fenced by `+++`:

```
+++
author    = "alice"
pubkey    = "Aa1b2c3d"
timestamp = "2026-02-17T10:00:00Z"
parent    = ""
signature = "<base64 Ed25519 signature>"
+++

Post body in Markdown.
```

The `parent` field holds the SHA-256 hex digest of the parent post file
content (empty string for root posts). The signature covers all front matter
fields except `signature` itself, serialized as sorted `key=value` lines
followed by a blank line and the raw body.

## Building

Requires Go 1.22 or later.

```sh
go build -o gitorum .
```

## Usage

### Generate an identity

```sh
gitorum keygen --username alice
```

Writes an Ed25519 keypair to `~/.config/gitorum/identity.toml` (mode 0600).
Use `--output` to override the path and `--force` to overwrite an existing key.

### Initialize a forum repository

```sh
gitorum init --name "My Forum" --username alice [--description "..."] [--dir .] [--remote <url>]
```

Creates `GITORUM.toml`, `keys/alice.pub`, and the first git commit.
Pass `--remote` to configure an `origin` remote for syncing.

### Start the server

```sh
gitorum serve [--port 8080] [--repo .]
```

Opens `http://localhost:8080` in your browser.  The web UI provides:

- Category and thread browsing
- Thread view with rendered Markdown and per-post signature badges
- Reply and new-thread forms
- Admin panel (visible only when the local key matches the forum admin key)
  for adding user keys and tombstoning posts

## Signature verification

On every read, each post's signature is verified against the key in
`keys/{author}.pub`. Posts are always displayed regardless of signature
status; invalid or missing signatures show a visible badge in the UI.

Only the admin key (stored in `GITORUM.toml`) may sign tombstone files
that mark posts as deleted.

## Sync model

Gitorum relies entirely on git for distribution. To pull updates from a
remote and push local posts:

```sh
# via the UI
click the sync button in the sidebar

# or via the API
curl http://localhost:8080/api/sync
```

On push conflict the binary performs `git pull --rebase` and retries once.
Because every post is an independent file, conflicts are rare and resolve
automatically.

## Configuration

On first run the setup page (served at `http://localhost:8080`) prompts for a
forum name, username, and optional remote URL. Configuration is stored in
`~/.config/gitorum/config.toml` (or `$XDG_CONFIG_HOME/gitorum/config.toml`).

## Dependencies

| Package | Purpose |
|---|---|
| `github.com/go-git/go-git/v5` | git operations |
| `github.com/spf13/cobra` | CLI |
| `github.com/BurntSushi/toml` | config and front matter |
| `github.com/yuin/goldmark` | server-side Markdown rendering |
| `crypto/ed25519` (stdlib) | signing and verification |

## License

GNU General Public License v3.0 or later. See [LICENSE](LICENSE).
