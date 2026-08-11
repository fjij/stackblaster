package cmd

import (
	"fmt"
	"os"
	"os/exec"
)

// ShouldPassthrough decides whether `args` (in the shape of os.Args, i.e.,
// with the program name at index 0) should be forwarded to git. It returns
// the args to pass to git (without the program name) and true when passthrough
// should fire, or nil/false to let cobra handle the invocation.
//
// The rule: if there is a first positional argument that isn't a flag and
// isn't one of sb's registered commands, forward everything after the program
// name to `git`.
func ShouldPassthrough(args []string) ([]string, bool) {
	if len(args) < 2 {
		return nil, false
	}
	first := args[1]
	if first == "" || first[0] == '-' {
		return nil, false
	}
	if IsKnownCommand(first) {
		return nil, false
	}
	return args[1:], true
}

// IsKnownCommand reports whether `name` matches any of sb's registered cobra
// commands or their aliases. `help` and `completion` are cobra built-ins
// added lazily by Execute; we treat them as known so passthrough doesn't
// swallow them.
func IsKnownCommand(name string) bool {
	if name == "help" || name == "completion" {
		return true
	}
	for _, c := range rootCmd.Commands() {
		if c.Name() == name {
			return true
		}
		for _, a := range c.Aliases {
			if a == name {
				return true
			}
		}
	}
	return false
}

// RunGit execs `git <args>` with stdio wired to the terminal and returns the
// process's exit code. On other failures (e.g., git missing) it prints a
// message and returns a non-zero code.
func RunGit(args []string) int {
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "sb: git is not on PATH")
		return 127
	}
	c := exec.Command("git", args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "sb:", err)
		return 1
	}
	return 0
}
