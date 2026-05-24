package main

import (
	"bytes"
	"io"
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
		// Structural errors
		"",
		"no-separators",
		"owner/repo-no-hash",
		"#-leading-hash/and-slash",
		"owner/repo#not-a-number",
		"https://gitlab.com/x/y/pull/1",
		// Character validation on Owner/Repo
		"a b/c#1",      // space in owner
		"o\n/r#1",      // newline in owner
		"o/r/extra#1",  // extra slash leaks into repo
		"o/../r#1",     // path traversal attempt
		"o%2Fevil/r#1", // URL-encoded slash in owner
		"o/r;evil#1",   // unexpected punctuation
		"<script>/r#1", // HTML-injection shape
		// Number must be positive
		"o/r#-5",
		"o/r#0",
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

func TestValidateBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		wantErr bool
	}{
		{"", false},
		{"https://api.github.com", false},
		{"https://api.github.com/", false},
		{"http://127.0.0.1:8080", false},
		{"http://localhost:1234", false},
		{"http://[::1]:9999", false},
		{"https://127.0.0.1:8443", false},
		{"https://attacker.example.com", true},
		{"https://api.github.com.evil.com", true},
		{"http://api.github.com", true}, // wrong scheme for non-loopback
		{"ftp://api.github.com", true},
		{"not a url at all", true},
		{"https://", true},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			err := validateBaseURL(tc.in)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateBaseURL(%q) error = %v, wantErr = %v", tc.in, err, tc.wantErr)
			}
		})
	}
}

func TestRun_RejectsHostileBaseURL(t *testing.T) {
	// Cannot run in parallel — t.Setenv requires the test to be non-parallel.
	t.Setenv("GITHUB_TOKEN", "any-token")
	t.Setenv("GITHUB_API_BASE_URL", "https://attacker.example.com")

	err := run([]string{"o/r#1"}, io.Discard)
	if err == nil {
		t.Fatal("expected error for hostile GITHUB_API_BASE_URL, got nil")
	}
	if !strings.Contains(err.Error(), "GITHUB_API_BASE_URL") {
		t.Errorf("error doesn't mention the offending env var: %v", err)
	}
}

// TestAuthTransport_DoesNotMutateRequest enforces the http.RoundTripper
// contract: implementations must not mutate the request they receive.
// Mutating it can leak the Authorization header into other request
// observers (e.g. errors that wrap *http.Request on redirect paths).
func TestAuthTransport_DoesNotMutateRequest(t *testing.T) {
	t.Parallel()

	seenAuth := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth <- r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	transport := &authTransport{token: "secret-token", base: http.DefaultTransport}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("test setup: original request already has Authorization=%q", got)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()

	if got := <-seenAuth; got != "Bearer secret-token" {
		t.Errorf("server saw Authorization=%q, want %q", got, "Bearer secret-token")
	}
	if got := req.Header.Get("Authorization"); got != "" {
		t.Errorf("authTransport mutated caller's request; Authorization=%q, want empty", got)
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

	// Run the binary against the fixture server. We build the child's env
	// from an explicit allowlist instead of inheriting os.Environ() so that
	// a real GITHUB_TOKEN in the test runner's environment cannot leak into
	// the subprocess.
	cmd := exec.Command(bin, "sarahmaeve/signatory#144")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}
	// Defensive: only one GITHUB_TOKEN entry — no inheritance leak.
	var tokenEntries int
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GITHUB_TOKEN=") {
			tokenEntries++
		}
	}
	if tokenEntries != 1 {
		t.Fatalf("cmd.Env has %d GITHUB_TOKEN entries; want exactly 1", tokenEntries)
	}
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
