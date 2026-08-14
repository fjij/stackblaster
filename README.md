# 🥞 stackblaster · `sb`

A [Graphite](https://graphite.dev)-flavored CLI for stacked pull requests, backed by plain `git` and GitHub's native [stacked PRs](https://docs.github.com/en/pull-requests/how-tos/stacked-pull-requests).

**Status:** alpha — I actively use it, but there may still be bugs. File an issue if needed.

## Why another stacking tool?

I liked `gt`'s ergonomics — `create`, `modify`, `submit`, `sync` — but didn't want the SaaS. GitHub finally shipped first-class stacked PRs, so the server side is solved. `sb` is the missing local half: a small Go binary that drives `git` (and, optionally, `gh`).

## Features

- **Local-first.** Only `sb submit` touches the remote — restack, move, and modify freely without pushing until you're ready.
- **One commit per branch.** `sb modify` amends the tip. Opt out if you want.
- **Auto-named branches.** `sb create -m "fix ingest retry"` gives you `fjij/2026-08-10-fix-ingest-retry`. Prefix and date format are configurable.
- **Keep the stack fresh.** `sb sync` fetches, fast-forwards trunk, restacks descendants, and prunes merged branches.
- **See it at a glance.** `sb log` renders the stack tree annotated with `(needs restack)` and `(needs submit)` hints.
- **GitHub is optional.** `sb submit` pushes with plain `git`. Install `gh` if you want it to also create PRs and link them into a stack on GitHub — skip it if you don't.

## Install

```sh
go install github.com/fjij/stackblaster/cmd/sb@latest
```

Requires `git` ≥ 2.40 on `PATH`. Optionally install `gh` ≥ 2.60 (with `gh auth login` done) if you want `sb submit` to create and link PRs on GitHub — without it, `sb submit` still pushes but skips the PR ops.

## Quick start

```sh
$ sb create -m "add retry to ingest"
✓ Created fjij/2026-08-14-add-retry-to-ingest (off main)
✓ Committed 3 file(s)

$ vim ingest.go
$ git add -p

$ sb modify
✓ fjij/2026-08-14-add-retry-to-ingest: a7beeba → 3f21c04

$ sb create -m "cover retry with a test"
✓ Created fjij/2026-08-14-cover-retry-with-a-test (off fjij/2026-08-14-add-retry-to-ingest)
✓ Committed 1 file(s)

$ sb submit
↑ pushing fjij/2026-08-14-add-retry-to-ingest
✓ opened fjij/2026-08-14-add-retry-to-ingest (base main) — https://github.com/you/repo/pull/4213
↑ pushing fjij/2026-08-14-cover-retry-with-a-test
✓ opened fjij/2026-08-14-cover-retry-with-a-test (base fjij/2026-08-14-add-retry-to-ingest) — https://github.com/you/repo/pull/4214
🔗 linked stack #1 (2 PRs) on GitHub

$ sb log
◉ fjij/2026-08-14-cover-retry-with-a-test  (current)
│
● fjij/2026-08-14-add-retry-to-ingest
│
◇ main
```

## Commands

| Command | Notes |
|---|---|
| `sb create [-m MSG]`     | New branch stacked on current; commits staged changes as one commit. |
| `sb modify [-m MSG]`     | Amend the current branch's tip and auto-restack descendants. Local-only. |
| `sb move [--onto TARGET]` | Change current branch's parent, rebase, restack descendants. Omit `--onto` in a TTY for a picker. |
| `sb submit [--ready] [--title X] [--body-file F]` | Push the stack; if `gh` is available, open a PR for each branch or retarget its base. Drafts by default. |
| `sb sync [--no-prune]`   | Fetch, fast-forward trunk, restack the tree, prune merged branches. |
| `sb continue`            | Resume after resolving a rebase conflict. |
| `sb log [--all]`         | Print the stack tree, annotated with `(needs restack)` / `(needs submit)` hints. |
| `sb checkout` / `sb co`  | Interactive branch picker. |
| `sb up [N]` · `sb down [N]` · `sb top` · `sb bottom` | Navigate the stack. |
| `sb track [--parent BRANCH]` · `sb untrack` | Adopt or drop existing branches. |
| `sb restack`             | Rebase descendants of the current branch onto its tip. |

Run `sb <cmd> --help` for details. Anything `sb` doesn't recognize is forwarded to `git`, so `sb status`, `sb push`, `sb rev-parse HEAD`, etc. Just Work.

Two names `sb` reuses from `git` with different meaning:

- `sb log` renders the **stack tree**, not commit history. Use `git log` for commits.
- `sb checkout` with no args opens a picker. With a branch name, it behaves like `git checkout`.

## Configuration

`sb` reads TOML from two files, later wins:

1. Global: `$XDG_CONFIG_HOME/sb/config.toml` (or `~/.config/sb/config.toml`)
2. Per-repo: `.sb/config.toml` at the repo root

Neither is created for you. Example:

```toml
# ~/.config/sb/config.toml
branch_prefix = "fjij"
```

### Keys

| Key                | Type    | Default        | Purpose |
|--------------------|---------|----------------|---------|
| `branch_prefix`    | string  | `""`           | Prepended to every auto-generated branch as `<prefix>/<date>-<slug>`. |
| `date_format`      | string  | `"2006-01-02"` | Go time format string for auto-generated branch names. |
| `trunk`            | string  | `"main"`       | The branch `sb` treats as sacred. |
| `force_with_lease` | bool    | `true`         | Whether `sb modify` / `sb submit` push with `--force-with-lease`. |
| `draft_by_default` | bool    | `true`         | Whether `sb submit` opens PRs as drafts. |

There is no `sb config` sub-command — edit the file directly.

## Non-goals

- Merging PRs. GitHub's UI + branch protection already do this.
- Enforcing single-commit branches. It's the default, not a hard rule.
- Cross-repo or cross-fork stacks.
- Anything requiring a login besides `gh auth login`.

## Development

There's a Nix flake with a devshell providing `go`, `gopls`, `gotools`, and `delve`:

```sh
nix develop
go build -o sb ./cmd/sb
go test ./...
```

Or bring your own toolchain — anything with `go` ≥ 1.25 works.

## License

MIT — see [LICENSE](LICENSE).
