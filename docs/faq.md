# 🥞 FAQ

## Why not just use `gt`?

Graphite is great, but it has a cloud component and its stacked PRs feature is separate from GitHub's native one. `sb` is offline-first and produces ordinary GitHub PRs — no account, no daemon, nothing beyond `git` and `gh`.

## Do I have to use one-commit-per-branch?

No. `sb modify -c` creates a new commit instead of amending, so multi-commit branches work fine. The single-commit shape is the default, not a hard rule.

## Where does sb store its state?

Only in git config: `branch.<name>.sbParent` for each tracked branch. That's it. If you `rm ~/go/bin/sb`, your branches and PRs still work.

## Can I mix `sb` and plain `git`?

Yes. Anything sb doesn't recognize is forwarded to `git` (with exit codes preserved), and sb's state lives in git config, which git respects. Use whichever's convenient.

## What if I want to see git's log, not the stack tree?

Just run `git log` — it's not intercepted. `sb log` shows the stack tree; `git log` still shows commit history.

## How does `sb submit` know which branch to base the PR on?

Each branch stores its parent as `branch.<name>.sbParent` in git config. `sb submit` walks the chain from the current branch down to trunk and calls `gh pr create --base <parent>` for each new PR, or `gh pr edit --base <parent>` if a PR already exists.
