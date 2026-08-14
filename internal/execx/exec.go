// Package execx wraps os/exec with the small conveniences every subprocess
// call in this repo needs: capture stdout/stderr, format errors as
// "<program> <args...>: <stderr>", trim trailing whitespace on the returned
// stdout. Used by gitx and ghx so their error messages look the same and
// their capture logic doesn't drift.
package execx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes name with args and returns trimmed stdout. On non-zero exit,
// the returned error is formatted as "<name> <args...>: <stderr>" (or
// "<name> <args...>: <err>" if stderr is empty).
func Run(name string, args ...string) (string, error) {
	return runWith(name, "", args)
}

// RunStdin is like Run but pipes body into the process's stdin. Useful for
// programs that read JSON on stdin (e.g. `gh api --input -`).
func RunStdin(name, body string, args ...string) (string, error) {
	return runWith(name, body, args)
}

// Interactive runs the command with the current terminal wired to
// stdin/stdout/stderr. Use this for anything that needs a real TTY
// (rebase, fetch, push).
func Interactive(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func runWith(name, stdin string, args []string) (string, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s: %s", strings.Join(append([]string{name}, args...), " "), msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}
