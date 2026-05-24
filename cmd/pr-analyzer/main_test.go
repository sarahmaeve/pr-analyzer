package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

func TestParsePRRef(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want analyzer.PRRef
	}{
		{"owner/repo#1", analyzer.PRRef{Owner: "owner", Repo: "repo", Number: 1}},
		{"sarahmaeve/signatory#144", analyzer.PRRef{Owner: "sarahmaeve", Repo: "signatory", Number: 144}},
		{"https://github.com/owner/repo/pull/1", analyzer.PRRef{Owner: "owner", Repo: "repo", Number: 1}},
		{"https://github.com/sarahmaeve/signatory/pull/144", analyzer.PRRef{Owner: "sarahmaeve", Repo: "signatory", Number: 144}},
		{"http://github.com/x/y/pull/2", analyzer.PRRef{Owner: "x", Repo: "y", Number: 2}},
		{"https://github.com/x/y/pull/3/", analyzer.PRRef{Owner: "x", Repo: "y", Number: 3}},
		{"  owner/repo#1  ", analyzer.PRRef{Owner: "owner", Repo: "repo", Number: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parsePRRef(tc.in)
			if err != nil {
				t.Fatalf("parsePRRef(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parsePRRef(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParsePRRef_Errors(t *testing.T) {
	t.Parallel()

	tests := []string{
		"",
		"no-separators",
		"owner/repo-no-hash",
		"#-leading-hash/and-slash",
		"owner/repo#not-a-number",
		"https://gitlab.com/x/y/pull/1",
	}
	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			if _, err := parsePRRef(tc); err == nil {
				t.Errorf("parsePRRef(%q) returned nil error, want error", tc)
			}
		})
	}
}

func TestSmoke_PR144(t *testing.T) {
	t.Parallel()

	// Build the binary.
	bin := filepath.Join(t.TempDir(), "pr-analyzer")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build failed: %v\n%s", err, out)
	}

	// Fixture server that serves PR #144's captured JSON.
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls/144", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "../../connectors/github/testdata/pr_144.json")
	})
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls/144/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "../../connectors/github/testdata/pr_144_files.json")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Run the binary against the fixture server.
	cmd := exec.Command(bin, "sarahmaeve/signatory#144")
	cmd.Env = append(os.Environ(),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL="+srv.URL,
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	out := stdout.String()

	// Each expected substring is something the deterministic pipeline must produce
	// given the captured PR #144 fixture.
	wants := []string{
		"PR #144 sarahmaeve https://github.com/sarahmaeve/signatory/pull/144",
		// 5631 adds + 220 deletes → scale 300, ceil(5631/300)=19 plus ceil(220/300)=1.
		"[+++++++++++++++++++-]  scale: 300 LOC/glyph",
		"adds: 5631  deletes: 220  files: 27",
		// signatory has at least one .go file in the changed set, so Go is in the languages line.
		"languages: ",
		"Go",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end with newline; last 20 bytes: %q", out[max(0, len(out)-20):])
	}
}
