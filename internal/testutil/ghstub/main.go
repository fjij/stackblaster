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
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	case "repo":
		if len(args) >= 2 && args[1] == "view" {
			// Return a fixed owner/repo. sb only cares that -q ".owner.login+…"
			// produces a well-formed "owner/name" string.
			fmt.Println("fake/fake")
			return
		}
		os.Exit(1)
	case "api":
		handleAPI(args[1:])
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

// --- gh api handling -----------------------------------------------------
//
// sb calls `gh api …` for the Stacked PRs REST endpoints. We recognize:
//   GET  repos/{owner}/{repo}/stacks?pull_request=N   → list (empty by default)
//   POST repos/{owner}/{repo}/stacks                   → create stack
//   POST repos/{owner}/{repo}/stacks/{n}/add           → append to stack
//
// State lives in $GH_STUB_STACKS_DIR/state.json.

type stackWire struct {
	Number       int              `json:"number"`
	PullRequests []stackPRWireItem `json:"pull_requests"`
}

type stackPRWireItem struct {
	Number int `json:"number"`
}

type stackStateFile struct {
	Next   int         `json:"next"`
	Stacks []stackWire `json:"stacks"`
}

func stacksStatePath() string {
	d := os.Getenv("GH_STUB_STACKS_DIR")
	if d == "" {
		d = filepath.Join(os.TempDir(), "gh-stub-stacks")
	}
	_ = os.MkdirAll(d, 0o755)
	return filepath.Join(d, "state.json")
}

func loadStacksState() *stackStateFile {
	data, err := os.ReadFile(stacksStatePath())
	if err != nil {
		return &stackStateFile{Next: 1}
	}
	var s stackStateFile
	if err := json.Unmarshal(data, &s); err != nil {
		return &stackStateFile{Next: 1}
	}
	if s.Next == 0 {
		s.Next = 1
	}
	return &s
}

func saveStacksState(s *stackStateFile) {
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(stacksStatePath(), b, 0o644)
}

// stackPathRe matches:
//   repos/OWNER/REPO/stacks
//   repos/OWNER/REPO/stacks?pull_request=N
//   repos/OWNER/REPO/stacks/N
//   repos/OWNER/REPO/stacks/N/add
//   repos/OWNER/REPO/stacks/N/unstack
var stackPathRe = regexp.MustCompile(`^repos/[^/]+/[^/]+/stacks(?:/(\d+)(?:/(add|unstack))?)?(?:\?pull_request=(\d+))?$`)

func handleAPI(args []string) {
	method := "GET"
	inputFromStdin := false
	var path string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--method", "-X":
			if i+1 < len(args) {
				method = strings.ToUpper(args[i+1])
				i++
			}
		case "-H", "--header":
			// Skip the header value.
			if i+1 < len(args) {
				i++
			}
		case "--input":
			if i+1 < len(args) && args[i+1] == "-" {
				inputFromStdin = true
				i++
			}
		case "-f", "-F", "--field":
			// Skip typed field value; we don't parse them.
			if i+1 < len(args) {
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "-") && path == "" {
				path = args[i]
			}
		}
	}
	if path == "" {
		os.Exit(1)
	}

	m := stackPathRe.FindStringSubmatch(path)
	if m == nil {
		// Unknown endpoint. Return an empty JSON object so callers don't crash.
		fmt.Println("{}")
		return
	}
	stackNumFromPath := m[1]
	action := m[2]
	prFilter := m[3]

	state := loadStacksState()

	// Read POST body if provided.
	var body map[string]any
	if inputFromStdin {
		raw, _ := io.ReadAll(os.Stdin)
		_ = json.Unmarshal(raw, &body)
	}

	switch {
	case method == "GET" && stackNumFromPath == "":
		// List stacks (possibly filtered by pull_request).
		var out []stackWire
		if prFilter != "" {
			pr, _ := strconv.Atoi(prFilter)
			for _, s := range state.Stacks {
				for _, p := range s.PullRequests {
					if p.Number == pr {
						out = append(out, s)
						break
					}
				}
			}
		} else {
			out = state.Stacks
		}
		if out == nil {
			out = []stackWire{}
		}
		b, _ := json.Marshal(out)
		os.Stdout.Write(b)
	case method == "POST" && stackNumFromPath == "" && action == "":
		// Create stack.
		prs := extractPRList(body)
		s := stackWire{Number: state.Next, PullRequests: prsToItems(prs)}
		state.Next++
		state.Stacks = append(state.Stacks, s)
		saveStacksState(state)
		b, _ := json.Marshal(s)
		os.Stdout.Write(b)
	case method == "POST" && stackNumFromPath != "" && action == "add":
		// Add PRs to an existing stack.
		n, _ := strconv.Atoi(stackNumFromPath)
		prs := extractPRList(body)
		updated := false
		for i, s := range state.Stacks {
			if s.Number == n {
				existing := s.PullRequests
				existing = append(existing, prsToItems(prs)...)
				state.Stacks[i].PullRequests = existing
				saveStacksState(state)
				b, _ := json.Marshal(state.Stacks[i])
				os.Stdout.Write(b)
				updated = true
				break
			}
		}
		if !updated {
			fmt.Fprintln(os.Stderr, "HTTP 404: stack not found")
			os.Exit(1)
		}
	default:
		fmt.Fprintln(os.Stderr, "HTTP 404: unhandled path in stub")
		os.Exit(1)
	}
}

func extractPRList(body map[string]any) []int {
	raw, ok := body["pull_requests"].([]any)
	if !ok {
		return nil
	}
	out := make([]int, 0, len(raw))
	for _, v := range raw {
		switch n := v.(type) {
		case float64:
			out = append(out, int(n))
		case int:
			out = append(out, n)
		}
	}
	return out
}

func prsToItems(prs []int) []stackPRWireItem {
	out := make([]stackPRWireItem, len(prs))
	for i, n := range prs {
		out[i] = stackPRWireItem{Number: n}
	}
	return out
}
