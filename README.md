# 🥞 stackblaster · `sb`

> A [Graphite](https://graphite.dev)-flavored CLI for stacked pull requests, backed by plain `git` and GitHub's native [stacked PRs](https://docs.github.com/en/pull-requests/how-tos/stacked-pull-requests). Stack 'em high, ship 'em hot. 🥞

No cloud. No daemon. No account. Just `git`, `gh`, and short stacks.

```
    🥞  ← top of stack (your latest change)
    🥞
    🥞  ← bottom (just above trunk)
   ═══  main
```

**Status:** early WIP. All core commands (`create`, `modify`, `move`, `submit`, `sync`, `continue`, `log`, `checkout`, nav, `track`/`untrack`) work in a temp-repo integration suite. Real-world use is untested.

## 🥞 Why another stacking tool?

I liked `gt`'s ergonomics — `create`, `modify`, `submit`, `sync` — but didn't want the SaaS. GitHub finally shipped first-class stacked PRs, so the server side is solved. `sb` is the missing local half: a small Go binary that drives `git` and `gh` the way `gt` drives its own backend.

## 🥞 Opinions

- **One commit per branch, by default.** `sb modify` amends the tip commit. The model doesn't _require_ one commit per branch — you can opt out — but the defaults assume it.
- **Only `sb submit` writes to the remote.** Every other command stays local, so you can restack, move, and modify freely without pushing until you're ready.
- **Auto-named branches.** `sb create -m "fix ingest retry"` gives you `fjij/2026-08-10-fix-ingest-retry`. Prefix and date format are configurable.
- **Drafts by default.** `sb submit` opens PRs as drafts unless `--ready`.
- **Trunk is sacred.** `sb` never commits to `main`; it always makes a branch.

## 🥞 Install

```sh
go install github.com/fjij/stackblaster/cmd/sb@latest
```

Requires:

- `git` ≥ 2.40 on `PATH`
- `gh` ≥ 2.60 on `PATH` (for `gh stack`), authenticated (`gh auth login`)

`sb` will print a helpful error if either is missing or misconfigured.

## 🥞 Quick start

```sh
$ sb create -m "add retry to ingest"
✓ Created fjij/2026-08-10-add-retry-to-ingest (off main)
✓ Committed 3 files

$ vim ingest.go
$ git add -p

$ sb modify
✓ Amended commit on fjij/2026-08-10-add-retry-to-ingest

$ sb create -m "cover retry with a test"
✓ Created fjij/2026-08-10-cover-retry-with-a-test (off fjij/2026-08-10-add-retry-to-ingest)

$ sb submit
✓ fjij/2026-08-10-add-retry-to-ingest    → PR #4213 (draft)
✓ fjij/2026-08-10-cover-retry-with-a-test → PR #4214 (draft), stacked on #4213

$ sb log
◉ fjij/2026-08-10-cover-retry-with-a-test   (current)
│
● fjij/2026-08-10-add-retry-to-ingest
│
◇ main
```

## 🥞 Commands

| Command | Notes |
|---|---|
| `sb create [-m MSG]`     | New branch stacked on current; commits staged changes as one commit. |
| `sb modify [-m MSG]`     | Amend the current branch's tip and auto-restack descendants. Local-only. |
| `sb move [--onto TARGET]` | Change current branch's parent, rebase, restack descendants. Omit `--onto` in a TTY for a picker. |
| `sb submit [--ready] [--title X] [--body-file F]` | Push the stack; open a PR for each branch or retarget its base. Drafts by default. `--title`/`--body-file` override the current branch's PR text (other branches use their commit message). |
| `sb sync [--no-prune]`   | Fetch, fast-forward trunk, restack the tree, prune merged branches. |
| `sb continue`            | Resume after resolving a rebase conflict. |
| `sb log [--all]`         | Print the full stack tree (rooted at trunk). `--all` also lists untracked and orphaned branches. |
| `sb checkout` / `sb co`  | Interactive branch picker (Bubble Tea). |
| `sb up [N]` · `sb down [N]` · `sb top` · `sb bottom` | Navigate the stack. Pass N to hop multiple branches; `up`/`down` prompt via picker at forks. |
| `sb track [--parent BRANCH]` · `sb untrack` | Adopt or drop existing branches. Omit `--parent` in a TTY for a picker. |
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

## 🥞 Configuration

sb reads TOML from two files and merges them in this order (later wins):

1. **Built-in defaults** (see table below).
2. **Global**: `$XDG_CONFIG_HOME/sb/config.toml`, or `~/.config/sb/config.toml`
   if `XDG_CONFIG_HOME` is unset.
3. **Per-repo**: `.sb/config.toml` at the repo root.

Neither file is created for you — drop one in when you want to override
a default. Example global config:

```toml
# ~/.config/sb/config.toml
branch_prefix = "fjij"
```

Example per-repo override (say the repo's trunk is `master`):

```toml
# <repo>/.sb/config.toml
trunk = "master"
```

### Keys

| Key                | Type    | Default        | Purpose |
|--------------------|---------|----------------|---------|
| `branch_prefix`    | string  | `""` (none)    | Prepended to every auto-generated branch as `<prefix>/<date>-<slug>`. With no prefix set, branches are just `<date>-<slug>`. |
| `date_format`      | string  | `"2006-01-02"` | Go time format string (see [pkg.go.dev/time](https://pkg.go.dev/time#pkg-constants)) used in auto-generated branch names. |
| `trunk`            | string  | `"main"`       | The branch sb treats as sacred. Never committed to directly; `sb sync` fast-forwards it from `origin`. |
| `force_with_lease` | bool    | `true`         | Whether `sb modify` / `sb submit` push with `--force-with-lease`. |
| `draft_by_default` | bool    | `true`         | Whether `sb submit` opens PRs as drafts. `--ready` and `--draft` on `sb submit` override per-invocation. |

There is intentionally no `sb config` sub-command — edit the file
directly. The defaults are chosen so an empty config works; the one you
almost certainly want to set is `branch_prefix`.

## 🥞 How it works

`sb` is a thin orchestrator:

- **Stack model** is stored in git config: `branch.<name>.sbParent` points at the parent branch. That's it. If you `rm ~/bin/sb`, your branches and PRs still work.
- **Local ops** shell out to `git` — no libgit2 dependency.
- **Remote ops** shell out to `gh` — `gh auth` handles credentials, and `gh stack` handles the PR-linking magic.

There is no server, no lockfile, and no state you can't inspect with `git config --list`.

## 🥞 Non-goals

- Merging PRs. GitHub's UI + branch protection rules already do this.
- Enforcing single-commit branches. It's the default, not a hard rule.
- Cross-repo or cross-fork stacks.
- Anything requiring a login besides `gh auth login`.

## 🥞 Development

There's a Nix flake with a devshell that provides `go`, `gopls`, `gotools`,
and `delve`:

```sh
nix develop
$ go build -o sb ./cmd/sb
$ go test ./...
```

With [direnv](https://direnv.net) + [nix-direnv](https://github.com/nix-community/nix-direnv),
`cd` into the repo activates the devshell automatically (the `.envrc` runs
`use flake`). First time:

```sh
direnv allow
```

Or bring your own toolchain — anything with `go` ≥ 1.25 works.

## 🥞 License

MIT — see [LICENSE](LICENSE).
