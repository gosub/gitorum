# Gitorum

A decentralized forum where all content is stored as signed files in a git
repository and distributed via standard git transports (HTTP, SSH, email
patches).

## Philosophy

Most forum software is a centralized service: one operator runs a database, sets
the rules, and controls who can speak. Users have no copy of the data and no
recourse if the service changes, paywalls, or disappears.

Gitorum is built on the opposite premise: the data belongs to the participants,
not the platform. Every post is a signed file in a plain git repository. The
repository can live on any machine, be replicated with any git hosting service,
and be mirrored by any participant. No account to create on a third-party site,
no vendor lock-in, no API terms of service.

The core design decisions follow from this premise:

- **Append-only by default.** Posts are new files; nothing is edited in place.
  Editing a post adds a new signed file that supersedes it (`supersedes` =
  hash of the original); readers see the latest version by the same author.
  The git log is the audit trail, and history rewriting is as visible as it is
  in any git repository.
- **Cryptographic authorship, not identity gatekeeping.** Anyone can write a
  post claiming any username. What matters is whether the post is signed with a
  key that the community trusts — the same model email and PGP have used for
  decades.
- **No runtime dependencies.** A single binary, a browser, and a git remote is
  all you need. There is no database process, no message queue, no container
  orchestrator.
- **Offline-first.** The local server works with no network access. Syncing is
  an explicit act, not a background service, so participants remain in control
  of when and where they share.
- **Legible storage.** The repository is readable without the Gitorum binary.
  Any text editor, `git log`, or language that can parse TOML can read the
  posts. The software can be replaced; the data cannot be held hostage by it.

## How it works

The binary starts a local HTTP server that serves a single-page web UI and a
JSON API. All forum data lives in a local git repository managed by the binary
using go-git. Users interact through the browser; git operations happen
transparently in the background.

Every post is an Ed25519-signed Markdown file committed to the repository.
Syncing with other participants is a fetch, a rebase of local commits, and a push.
There is no central server, no database, and no account system beyond a keypair
stored in `~/.config/gitorum/identity.toml`.

## Repository layout

The forum data format is a plain directory tree inside a git repository:

```
/
├── GITORUM.toml                    forum name, description, admin public key
├── keys/
│   ├── {username}.pub              user's Ed25519 public keys (base64, one per line)
│   └── {username}.sig              admin signature certifying {username}.pub
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
title     = "Thread title"
signature = "<base64 Ed25519 signature>"
+++

Post body in Markdown.
```

The `parent` field holds the SHA-256 hex digest of the file of the post being
answered (empty string for root posts). `title` is present on root posts.
Optional fields such as `title` are signed only when present, so older posts
without them still verify. The signature covers all front matter
fields except `signature` itself, serialized as sorted `key=value` lines
followed by a blank line and the raw body.

## Building

Requires Go 1.22 or later.

```sh
make        # builds build/gitorum
make test   # runs all tests
make clean  # removes build/
```

## Command line usage

### `gitorum keygen`

```sh
gitorum keygen --username alice [--output ~/.config/gitorum/identity.toml] [--force]
```

Generates an Ed25519 keypair and writes it to
`~/.config/gitorum/identity.toml` (mode 0600, respects `$XDG_CONFIG_HOME`).
Use `--output` to specify an alternative path and `--force` to overwrite an
existing file.

### `gitorum init`

```sh
gitorum init --name "My Forum" [--description "..."] [--username alice] [--dir .] [--remote <url>]
```

Creates a new forum repository in the current directory (or `--dir`):
writes `GITORUM.toml` and `keys/<username>.pub`, and makes the first git
commit. Pass `--remote` to configure an `origin` remote immediately.

### `gitorum serve`

```sh
gitorum serve [--port 8080] [--listen 127.0.0.1] [--repo .] [--identity ~/.config/gitorum/identity.toml]
```

Starts the local HTTP server. Open `http://localhost:8080` in your browser.

The API has no login: whoever can reach it can post with your key. It
therefore binds to `127.0.0.1` by default, and rejects cross-origin requests,
non-JSON writes, and requests for non-loopback host names (DNS rebinding).
Pass `--listen 0.0.0.0` only on a network you fully trust.
The web UI provides:

- Category and thread browsing
- Thread view with rendered Markdown and per-post signature badges
- Reply and new-thread forms
- Admin panel (visible only when the local key matches the forum admin key)
  for adding user keys, creating categories, and hiding posts. Hiding writes
  an admin-signed tombstone; the post file stays in the repository (and its
  history) unless you also choose to purge it, which removes it from the
  current tree only. Nothing can remove a post from copies others already
  have.

If the forum has not been initialized yet, the browser will show a setup
wizard instead of the forum.

### `gitorum clone`

```sh
gitorum clone <url> [dir]
```

Clones a remote forum repository and pins its admin key locally. The
directory defaults to the last path component of the URL with `.git`
stripped. After cloning, run
`gitorum serve --repo <dir>`.

### `gitorum config`

```sh
gitorum config [--repo .] [--name "New Name"] [--remote <url>] [--auto-approve]
```

Without flags, prints the current forum name, admin key, remote URL, and
auto-approve setting.
With `--name`, updates the forum display name (commits the change).
With `--remote`, sets the `origin` remote URL.
With `--auto-approve`, enables automatic approval of join requests when the
admin runs sync.

### `gitorum verify`

```sh
gitorum verify [--repo .]
```

Checks every key certificate, post signature and tombstone against the
pinned admin key. Exits with status 1 if anything fails.

### `gitorum certify`

```sh
gitorum certify <username>... [--repo .] [--identity <path>] [--no-push]
```

Admin only. Signs existing `keys/<username>.pub` files so their posts are
trusted. Verify each key with its owner first.

### `gitorum request`

```sh
gitorum request [--repo .] [--identity <path>] [--print]
```

Asks the admin to register your key. Every request is signed with your key,
proving you hold it; requests without a valid signature cannot be approved.

- Without `--print`: commits `requests/<username>.pub` to a cloned forum and
  pushes it. This needs push access to the forum remote.
- With `--print`: writes nothing and prints a one-line token
  (`gitorum-join:…`). Send it to the admin through any channel. This is the
  way in when you cannot push to the remote.

### `gitorum approve`

```sh
gitorum approve <username | join-token> [--repo .] [--identity <path>] [--no-push]
```

Admin only. Registers and certifies the key from a pending request or a
join token. The admin panel offers the same actions.

## Mini tutorial

The following shows how to start a fresh forum and invite a second
participant to join.

**1. Build the binary**

```sh
make   # output: build/gitorum
```

**2. Create your identity**

```sh
./build/gitorum keygen --username alice
```

This writes your keypair to `~/.config/gitorum/identity.toml`. Keep the
private key private; share the public key freely.

**3. Create an empty git repository on a host everyone can reach**

Any git hosting service works — GitHub, Codeberg, a bare repo on your own
server, etc. Create an empty repository and copy its URL.

**4. Initialize the forum**

```sh
mkdir my-forum && cd my-forum
../build/gitorum init --name "My Forum" --remote git@github.com:alice/my-forum.git
```

This commits `GITORUM.toml` and `keys/alice.pub`, then pushes to the remote.

**5. Start the server**

```sh
../build/gitorum serve --repo .
```

Open `http://localhost:8080`. You are now the forum admin.

**6. Invite a second participant**

Bob generates his own identity on his machine and clones the forum:

```sh
gitorum keygen --username bob
gitorum clone git@github.com:alice/my-forum.git
```

Bob then submits a join request. If he can push to the forum remote:

```sh
gitorum request --repo my-forum
# Join request submitted for @bob
# Pushed to remote. The forum admin will see the request on their next sync.
```

Alice clicks the sync button in the sidebar. The admin panel shows a pending
count on the "Join Requests" button. Alice clicks it, reviews Bob's public
key, and clicks Approve. The key and Alice's certificate for it are
committed to `keys/` and pushed.

If Bob cannot push, he runs `gitorum request --print` and sends Alice the
token; she pastes it into "Add User Key" or runs `gitorum approve <token>`.

Alternatively, Alice can enable automatic approval so signed join requests
are approved whenever she syncs:

```sh
gitorum config --repo my-forum --auto-approve
```

Auto-approval lets anyone who can push to the remote become a trusted
member under any unused username, so only enable it when push access is
itself restricted to people you trust.

Bob starts his own local server and syncs to pick up the approval:

```sh
gitorum serve --repo my-forum
# click sync in the sidebar
```

Bob can now read, reply, and start threads. His posts are signed with his
key, and because the admin certified it on approval they show a "signed"
badge for every participant.

## Trust model

Signatures only mean something if the keys they are checked against can be
trusted, and anyone with push access can edit any file in the repository.
Gitorum therefore anchors trust outside the shared repository:

- **Pinned admin key.** The admin key is recorded in your local
  `.git/config` (`[gitorum] adminkey`) by `init`, `clone`, or the first time
  `serve` opens a repository (trust on first use). Editing `admin_pubkey` in
  `GITORUM.toml` does not change who the admin is; the UI shows a warning
  instead. Confirm the fingerprint printed by `clone` with the admin.
- **Certified user keys.** `keys/<user>.pub` lists a user's keys, one per
  line. It is trusted only when `keys/<user>.sig` holds the admin's
  signature over it (approving a request or adding a key writes both).
  Several keys per user allow rotation: old posts keep verifying.
- **Admin-only tombstones.** A `.tomb` file hides a post only if the pinned
  admin key signed it and its `parent` is the hash of that exact post.

Each post shows a badge: **signed** (certified key), **untrusted key** (valid
signature, but the key was never certified by the admin), **invalid sig**,
or **no key**. Posts are always displayed regardless of status.

`gitorum verify` audits the whole repository and lists every problem. Forums
created before key certificates existed can be migrated with
`gitorum certify <user>...` after checking each key with its owner.

The admin key itself cannot yet be rotated or shared between several admins;
losing it means re-creating the forum's trust root by hand.

## Sync model

Gitorum relies entirely on git for distribution. To pull updates from a
remote and push local posts:

```sh
# via the UI
click the sync button in the sidebar

# or via the API
curl -X POST -H 'Content-Type: application/json' http://localhost:8080/api/sync
```

A sync fetches `origin`, then integrates it:

- if only the remote moved, the local branch fast-forwards;
- if both sides have new commits (e.g. two people posted while offline),
  the local commits are replayed on top of the remote, like
  `git pull --rebase`, and then pushed. If someone else pushed in the
  meantime, the cycle is retried once.

Every post is a new, uniquely named file, so replaying never conflicts in
practice. A real conflict (both sides edited the same file, such as
`GITORUM.toml`, differently) aborts the sync, leaves the local branch
exactly as it was, and is reported in the UI.

Posting also syncs. If that fails (no network, conflict), the post stays
committed locally and the sidebar shows how many commits are unpushed.

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
