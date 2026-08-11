# 🥞 Using `sb` from AI agents

`sb` is designed to be usable non-interactively, which makes it a reasonable fit for coding agents (Claude Code, Codex, etc.) that manage branches and PRs on your behalf.

## Non-interactive flags

Every interactive picker in `sb` has a flag equivalent, so agents can avoid TTY prompts entirely:

- `sb checkout <branch>` — bypasses the picker.
- `sb track --parent <branch>` — bypasses the parent picker.
- `sb move --onto <branch>` — bypasses the target picker.
- `sb up <N>` — auto-navigates when only one branch lives at distance N.

If a picker would fire without a TTY (e.g., an ambiguous `sb up` at a fork), sb exits with a clear error mentioning which flag to pass instead. No process ever hangs on stdin.

## Inspecting stack state from a script

`sb` doesn't emit JSON today. To read the stack from a script:

- Current branch: `git branch --show-current`
- Parent of a branch: `git config --get branch.<name>.sbParent`
- All tracked branches: `git config --get-regexp '^branch\..*\.sbparent$'`

## Overrides on `sb submit`

For agents that want to write proper PR titles/bodies instead of using the commit message verbatim:

```sh
sb submit --title "Add retry to ingest" --body-file /tmp/pr-body.md
```

These apply only to the *current* branch's PR when it's being created. Other branches in the stack use their commit-derived text, and PRs that already exist aren't touched.

## Local-only vs. remote

Only `sb submit` writes to the remote. Every other command (`create`, `modify`, `restack`, `move`, `sync`, `continue`, navigation) stays local, so agents can build and revise a stack freely and push only when they're ready.
