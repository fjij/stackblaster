# stackblaster · `sb`

> A [Graphite](https://graphite.dev)-flavored CLI for stacked pull requests, backed by plain `git` and GitHub's native [stacked PRs](https://docs.github.com/en/pull-requests/how-tos/stacked-pull-requests).

No cloud. No daemon. No account. Just `git`, `gh`, and opinions.

**Status:** early WIP. All core commands (`create`, `modify`, `move`, `submit`, `sync`, `continue`, `log`, `checkout`, nav, `track`/`untrack`) work in a temp-repo integration suite. Real-world use is untested.

## Why another stacking tool?

I liked `gt`'s ergonomics — `create`, `modify`, `submit`, `sync` — but didn't want the SaaS. GitHub finally shipped first-class stacked PRs, so the server side is solved. `sb` is the missing local half: a small Go binary that drives `git` and `gh` the way `gt` drives its own backend.

## Opinions

- **One commit per branch, by default.** `sb modify` amends and force-pushes with lease. The model doesn't _require_ one commit per branch — you can opt out — but the defaults assume it.
- **Auto-named branches.** `sb create -m "fix ingest retry"` gives you `wharris/2026-08-10-fix-ingest-retry`. Prefix and date format are configurable.
- **Drafts by default.** `sb submit` opens PRs as drafts unless `--ready`.
- **Trunk is sacred.** `sb` never commits to `main`; it always makes a branch.

## Install

```sh
go install github.com/fjij/stackblaster@latest
```

Requires:

- `git` ≥ 2.40 on `PATH`
- `gh` ≥ 2.60 on `PATH` (for `gh stack`), authenticated (`gh auth login`)

`sb` will print a helpful error if either is missing or misconfigured.

## Quick start

```sh
$ sb create -m "add retry to ingest"
✓ Created wharris/2026-08-10-add-retry-to-ingest (off main)
✓ Committed 3 files

$ vim ingest.go
$ git add -p

$ sb modify
✓ Amended commit on wharris/2026-08-10-add-retry-to-ingest

$ sb create -m "cover retry with a test"
✓ Created wharris/2026-08-10-cover-retry-with-a-test (off wharris/2026-08-10-add-retry-to-ingest)

$ sb submit
✓ wharris/2026-08-10-add-retry-to-ingest    → PR #4213 (draft)
✓ wharris/2026-08-10-cover-retry-with-a-test → PR #4214 (draft), stacked on #4213

$ sb log
◉ wharris/2026-08-10-cover-retry-with-a-test   (current)
│
● wharris/2026-08-10-add-retry-to-ingest
│
◇ main
```

## Commands

| Command | Notes |
|---|---|
| `sb create [-m MSG]`     | New branch stacked on current; commits staged changes as one commit. |
| `sb modify [-m MSG]`     | Amend the current branch's tip; auto-restack descendants; force-push-with-lease. |
| `sb move --onto TARGET`  | Change current branch's parent to TARGET, rebase, restack descendants. |
| `sb submit [--ready]`    | Push the stack; open a PR for each branch or retarget its base. Drafts by default. |
| `sb sync [--no-prune]`   | Fetch, fast-forward trunk, restack the tree, prune merged branches. |
| `sb continue`            | Resume after resolving a rebase conflict. |
| `sb log [--all]`         | Print the stack tree. |
| `sb checkout` / `sb co`  | Interactive branch picker (Bubble Tea). |
| `sb up` · `sb down` · `sb top` · `sb bottom` | Navigate the stack. |
| `sb track` · `sb untrack` | Adopt or drop existing branches. |
| `sb restack`             | Rebase descendants of the current branch onto its tip. |

Run `sb <cmd> --help` for details.

Anything sb doesn't recognize is forwarded to `git` with the exit code
preserved, so `sb status`, `sb push`, `sb rev-parse HEAD`, etc. Just Work
without having to switch tools:

```sh
$ sb status
On branch main
nothing to commit, working tree clean
$ sb rev-parse HEAD
a7beebafa0ce5ac27b9f286fc4a5128bda4ebf21
```

Note the two names sb reuses from git with different meaning:

- `sb log` renders the **stack tree**, not commit history. Use `git log`
  (or `sb --help log`) if you want commits.
- `sb checkout` with no args opens a picker over the current stack.
  With a branch name, it behaves like `git checkout <branch>`.

## Configuration

`~/.config/sb/config.toml`:

```toml
branch_prefix    = "wharris"
date_format      = "2006-01-02"    # Go time format
trunk            = "main"
force_with_lease = true
draft_by_default = true
```

Per-repo overrides live in `.sb/config.toml`.

## How it works

`sb` is a thin orchestrator:

- **Stack model** is stored in git config: `branch.<name>.sbParent` points at the parent branch. That's it. If you `rm ~/bin/sb`, your branches and PRs still work.
- **Local ops** shell out to `git` — no libgit2 dependency.
- **Remote ops** shell out to `gh` — `gh auth` handles credentials, and `gh stack` handles the PR-linking magic.

There is no server, no lockfile, and no state you can't inspect with `git config --list`.

## Non-goals

- Merging PRs. GitHub's UI + branch protection rules already do this.
- Enforcing single-commit branches. It's the default, not a hard rule.
- Cross-repo or cross-fork stacks.
- Anything requiring a login besides `gh auth login`.

## Development

```sh
# with nix
nix shell nixpkgs#go --command go build -o sb .
nix shell nixpkgs#go --command go test ./...

# or with a system go toolchain
go build -o sb .
go test ./...
```

## License

MIT — see [LICENSE](LICENSE).
