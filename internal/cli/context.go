package cli

import (
	"errors"
	"fmt"

	"github.com/fjij/stackblaster/internal/config"
	"github.com/fjij/stackblaster/internal/gitx"
	"github.com/fjij/stackblaster/internal/stack"
)

// runCtx is the standard bootstrap every sb command needs: git preflight,
// repo root, config, and (best-effort) current branch. Current is empty
// when the caller is in detached HEAD — commands that need a branch should
// call requireCurrent.
type runCtx struct {
	RepoRoot string
	Cfg      config.Config
	Current  string
}

// loadContext runs Preflight + RepoRoot + config.Load + CurrentBranch.
// A detached-HEAD state does not error; it just leaves ctx.Current == "".
func loadContext() (*runCtx, error) {
	if err := gitx.Preflight(); err != nil {
		return nil, err
	}
	repoRoot, err := gitx.RepoRoot()
	if err != nil {
		return nil, errors.New("must be run inside a git repository")
	}
	cfg, err := config.Load(repoRoot)
	if err != nil {
		return nil, err
	}
	current, _ := gitx.CurrentBranch()
	return &runCtx{RepoRoot: repoRoot, Cfg: cfg, Current: current}, nil
}

// loadStackContext is loadContext plus stack.Load(cfg.Trunk).
func loadStackContext() (*runCtx, *stack.Stack, error) {
	ctx, err := loadContext()
	if err != nil {
		return nil, nil, err
	}
	s, err := stack.Load(ctx.Cfg.Trunk)
	if err != nil {
		return nil, nil, err
	}
	return ctx, s, nil
}

// requireCurrent returns a helpful error if the caller isn't on a branch.
// Commands that mutate the current branch (create, modify, submit, etc.)
// call this right after loadContext.
func (c *runCtx) requireCurrent() error {
	if c.Current == "" {
		return fmt.Errorf("not on a branch — sb needs a checked-out branch to operate on")
	}
	return nil
}
