// ghstub is a fake `gh` binary used by sb's tests. It doesn't talk to
// GitHub; it reads and writes JSON fixture files on disk and logs every
// invocation to a file so tests can assert on the calls that were made.
//
// State is entirely env-driven so a single build can serve every test:
//
//   GH_STUB_LOG       Path to append each invocation's argv to (one line per call).
//   GH_STUB_PRS_DIR   Directory containing PR fixture files (<branch>.json). Also
//                     where `pr create` writes new PR fixtures.
//   GH_STUB_MERGED    Comma-separated list of branch names that `pr list --state
//                     merged` should return.
//   GH_STUB_UNAUTHED  If set, `gh auth status` exits 1 instead of 0.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type prState struct {
	Number      int    `json:"number"`
	State       string `json:"state"`
	Draft       bool   `json:"isDraft"`
	BaseRefName string `json:"baseRefName"`
	HeadRefName string `json:"headRefName"`
	URL         string `json:"url"`
	Title       string `json:"title"`
}

func main() {
	// Log the invocation for tests to assert on.
	if logPath := os.Getenv("GH_STUB_LOG"); logPath != "" {
		if f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintln(f, strings.Join(os.Args[1:], "\t"))
			f.Close()
		}
	}
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(1)
	}
	switch args[0] {
	case "auth":
		if os.Getenv("GH_STUB_UNAUTHED") != "" {
			os.Exit(1)
		}
		os.Exit(0)
	case "pr":
		if len(args) < 2 {
			os.Exit(1)
		}
		switch args[1] {
		case "view":
			handlePRView(args[2:])
		case "create":
			handlePRCreate(args[2:])
		case "edit":
			handlePREdit(args[2:])
		case "list":
			handlePRList(args[2:])
		default:
			os.Exit(1)
		}
	default:
		os.Exit(1)
	}
}

func prsDir() string {
	if d := os.Getenv("GH_STUB_PRS_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.TempDir(), "gh-stub-prs")
}

func handlePRView(args []string) {
	if len(args) == 0 {
		os.Exit(1)
	}
	branch := args[0]
	path := filepath.Join(prsDir(), branch+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, `no pull requests found for branch "%s"`+"\n", branch)
		os.Exit(1)
	}
	os.Stdout.Write(data)
}

func handlePRCreate(args []string) {
	var head, base, title, body string
	var draft bool
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--head":
			head = args[i+1]
			i++
		case "--base":
			base = args[i+1]
			i++
		case "--title":
			title = args[i+1]
			i++
		case "--body":
			body = args[i+1]
			i++
		case "--draft":
			draft = true
		}
	}
	_ = body
	dir := prsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	// Assign a fake number based on how many PR fixtures already exist. Not
	// realistic but stable and orders correctly.
	files, _ := os.ReadDir(dir)
	num := 4200 + len(files)
	pr := prState{
		Number:      num,
		State:       "OPEN",
		Draft:       draft,
		BaseRefName: base,
		HeadRefName: head,
		URL:         fmt.Sprintf("https://github.com/fake/fake/pull/%d", num),
		Title:       title,
	}
	b, _ := json.Marshal(pr)
	path := filepath.Join(dir, head+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(pr.URL)
}

func handlePREdit(args []string) {
	if len(args) == 0 {
		os.Exit(1)
	}
	branch := args[0]
	var base string
	for i := 1; i < len(args); i++ {
		if args[i] == "--base" && i+1 < len(args) {
			base = args[i+1]
			i++
		}
	}
	path := filepath.Join(prsDir(), branch+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		os.Exit(1)
	}
	var pr prState
	if err := json.Unmarshal(data, &pr); err != nil {
		os.Exit(1)
	}
	if base != "" {
		pr.BaseRefName = base
	}
	b, _ := json.Marshal(pr)
	os.WriteFile(path, b, 0o644)
}

func handlePRList(_ []string) {
	merged := os.Getenv("GH_STUB_MERGED")
	if merged == "" {
		return
	}
	for _, b := range strings.Split(merged, ",") {
		if b = strings.TrimSpace(b); b != "" {
			fmt.Println(b)
		}
	}
}
