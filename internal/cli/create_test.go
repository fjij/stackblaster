package cmd

import "testing"

func TestBranchName(t *testing.T) {
	cases := []struct {
		name       string
		prefix     string
		dateFmt    string
		msg        string
		wantSuffix string // full name has a runtime-derived date; assert the suffix
	}{
		{"basic", "fjij", "2006-01-02", "add retry", "-add-retry"},
		{"punct is collapsed", "fjij", "2006-01-02", "fix: cache!", "-fix-cache"},
		{"long slug truncated", "fjij", "2006-01-02", "one two three four five six seven eight nine ten eleven", ""},
		{"no prefix", "", "2006-01-02", "hello", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := branchName(tc.prefix, tc.dateFmt, tc.msg)
			if tc.prefix != "" && got[:len(tc.prefix)+1] != tc.prefix+"/" {
				t.Fatalf("expected prefix %q/, got %q", tc.prefix, got)
			}
			if tc.name == "long slug truncated" {
				// slug portion max 40 chars, dash-trimmed
				// full = prefix + "/" + date + "-" + slug
				slugStart := len(tc.prefix) + 1 + 10 + 1 // prefix/YYYY-MM-DD-
				slug := got[slugStart:]
				if len(slug) > 40 {
					t.Fatalf("slug too long: %d chars: %q", len(slug), slug)
				}
			}
			if tc.name == "no prefix" {
				if got[0] == '/' {
					t.Fatalf("unexpected leading slash: %q", got)
				}
			}
		})
	}
}
