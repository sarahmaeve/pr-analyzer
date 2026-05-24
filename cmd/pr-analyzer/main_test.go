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
	"sync/atomic"
	"testing"

	"github.com/alecthomas/kong"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

// buildBinary compiles cmd/pr-analyzer into t.TempDir() and returns the
// path. Marked t.Helper so failure points report at the caller's line.
func buildBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "pr-analyzer")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build failed: %v\n%s", err, out)
	}
	return bin
}

// pr144FixtureServer stands up an httptest server that serves the
// captured PR #144 JSON for both the detail and files endpoints. The
// server is closed via t.Cleanup. Used by the slice-1 smoke test and
// the slice-2 config-driven smoke tests.
func pr144FixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
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
	return srv
}

// prTrapdoorFixtureServer stands up an httptest server that serves
// the fabricated Trapdoor-shape fixture (agentforge/copilot-toolkit#47)
// for both the detail and files endpoints. The fixture models the
// PR-against-legit-AI-project propagation vector from
// signatory/design/threat-landscape/2026-05-24-trapdoor-crypto-stealer.md.
func prTrapdoorFixtureServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/agentforge/copilot-toolkit/pulls/47", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "../../connectors/github/testdata/pr_trapdoor.json")
	})
	mux.HandleFunc("/repos/agentforge/copilot-toolkit/pulls/47/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "../../connectors/github/testdata/pr_trapdoor_files.json")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestResolveLocalCloneDir pins the precedence ladder for slice 3:
// --local-clone-dir CLI flag > local_clone_dir YAML > CWD. Relative
// flag values resolve against CWD; YAML values have already been
// resolved against the config-file directory by the loader.
func TestResolveLocalCloneDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		flagValue string
		yamlValue string
		cwd       string
		want      string
	}{
		{"flag wins over YAML", "/flag/path", "/yaml/path", "/cwd", "/flag/path"},
		{"flag empty, YAML wins", "", "/yaml/path", "/cwd", "/yaml/path"},
		{"both empty, CWD default", "", "", "/cwd", "/cwd"},
		{"relative flag resolves against CWD", "rel", "", "/cwd", "/cwd/rel"},
		{"absolute flag passes through unchanged", "/abs", "", "/cwd", "/abs"},
		{"relative flag wins over absolute YAML and resolves against CWD", "rel", "/yaml/abs", "/cwd", "/cwd/rel"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := resolveLocalCloneDir(tc.flagValue, tc.yamlValue, tc.cwd)
			if got != tc.want {
				t.Errorf("resolveLocalCloneDir(%q, %q, %q) = %q, want %q",
					tc.flagValue, tc.yamlValue, tc.cwd, got, tc.want)
			}
		})
	}
}

func TestKongParse_LocalCloneDir_AcceptsExistingDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	var args cliArgs
	parser, err := kong.New(&args)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	if _, err = parser.Parse([]string{"--local-clone-dir", tempDir, "o/r#1"}); err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if args.LocalCloneDir != tempDir {
		t.Errorf("LocalCloneDir = %q, want %q", args.LocalCloneDir, tempDir)
	}
	if args.PR != "o/r#1" {
		t.Errorf("PR = %q, want %q", args.PR, "o/r#1")
	}
}

func TestKongParse_LocalCloneDir_RejectsMissingDir(t *testing.T) {
	t.Parallel()

	nonExistent := filepath.Join(t.TempDir(), "does-not-exist")
	var args cliArgs
	parser, err := kong.New(&args)
	if err != nil {
		t.Fatalf("kong.New: %v", err)
	}
	_, err = parser.Parse([]string{"--local-clone-dir", nonExistent, "o/r#1"})
	if err == nil {
		t.Fatal("expected error for non-existent directory, got nil")
	}
	if !strings.Contains(err.Error(), "does-not-exist") {
		t.Errorf("error %v does not mention the bad path", err)
	}
}

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

	// Each case asserts which error category the parser landed in, not just
	// "err != nil". A regression that produces an error for the wrong reason
	// (e.g., structural check catches an input that should have been caught
	// by character validation) will fail here even though the input still
	// produces some error.
	tests := []struct {
		in              string
		wantErrContains string
	}{
		// Structural errors — caller failed to provide a recognizable form.
		{"", "usage:"},
		{"no-separators", "usage:"},
		{"owner/repo-no-hash", "usage:"},
		{"#-leading-hash/and-slash", "usage:"},
		{"https://gitlab.com/x/y/pull/1", "usage:"},
		// Number parsing inside the short-form parser.
		{"owner/repo#not-a-number", "invalid PR number after"},
		// Character validation on Owner.
		{"a b/c#1", "invalid owner"},
		{"o\n/r#1", "invalid owner"},
		{"o%2Fevil/r#1", "invalid owner"},
		{"<script>/r#1", "invalid owner"},
		// Character validation on Repo.
		{"o/r/extra#1", "invalid repo"},
		{"o/../r#1", "invalid repo"},
		{"o/r;evil#1", "invalid repo"},
		// Number range — Atoi succeeds but validateRef rejects.
		{"o/r#-5", "PR number must be positive"},
		{"o/r#0", "PR number must be positive"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			_, err := parsePRRef(tc.in)
			if err == nil {
				t.Fatalf("parsePRRef(%q) returned nil, want error containing %q", tc.in, tc.wantErrContains)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("parsePRRef(%q) error = %v, want substring %q", tc.in, err, tc.wantErrContains)
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

	err := run(cliArgs{PR: "o/r#1"}, io.Discard, io.Discard)
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

	bin := buildBinary(t)
	srv := pr144FixtureServer(t)

	// Run the binary against the fixture server. Env is an explicit allowlist
	// rather than os.Environ() so a real GITHUB_TOKEN in the parent cannot
	// leak into the child; the dedicated TestSmoke_DoesNotInheritParentGitHubToken
	// test verifies that property end-to-end.
	cmd := exec.Command(bin, "sarahmaeve/signatory#144")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
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
		"PR #144 sarahmaeve https://github.com/sarahmaeve/signatory/pull/144\n",
		// 5631 adds + 220 deletes → scale 300, ceil(5631/300)=19 plus ceil(220/300)=1.
		"[+++++++++++++++++++-]  scale: 300 LOC/glyph\n",
		"adds: 5631  deletes: 220  files: 27\n",
		"tests touched\n",
		"no dependency manifest touched\n",
		"languages: Go, Markdown\n",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	// PR #144's author_association is OWNER — in the trusted allowlist,
	// so the engineer-profile bullet must NOT appear. A regression that
	// surfaces the bullet for trusted associations would trip here.
	if strings.Contains(out, "author association:") {
		t.Errorf("unexpected 'author association:' bullet for trusted OWNER association in output:\n%s", out)
	}

	if !strings.HasSuffix(out, "\n") {
		t.Errorf("output does not end with newline; last 20 bytes: %q", out[max(0, len(out)-20):])
	}

	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr on success, got:\n%s", stderr.String())
	}
}

// TestSmoke_PR144_WithConfig exercises the --config end-to-end path:
// project config on disk, binary picks it up, every slice-2 knob that
// the config drives produces an observable change in the rendered
// output.
func TestSmoke_PR144_WithConfig(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := pr144FixtureServer(t)

	// max_loc: 100 will trip on PR #144's 5851 total LOC.
	// risky_paths: cmd matches files like cmd/signatory/collectors.go.
	// languages.preferred: [Go] surfaces Go in the preferred bucket.
	// render.bar_scale: 500 overrides auto-scale (which would otherwise
	// land at 300 for this PR), so the scale notice changes too.
	cfgBody := `render:
  bar_scale: 500
codeshape:
  risky_paths: [cmd]
  max_loc: 100
  languages:
    preferred: [Go]
`
	cfgPath := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cmd := exec.Command(bin, "--config", cfgPath, "sarahmaeve/signatory#144")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	out := stdout.String()
	wants := []string{
		// BarScale override actually applied (not auto-scaled to 300).
		"scale: 500 LOC/glyph",
		// Language posture bucket.
		"languages preferred: Go\n",
		// Risky paths matched at least one cmd/ file.
		"risky paths touched: ",
		"cmd/signatory/collectors.go",
		// max_loc threshold exceeded.
		"exceeds max LOC: 5851 > 100\n",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}
}

// TestSmoke_PR144_DiscoversConfig drops pr-analyzer.yaml in a temp dir
// and invokes the binary from a nested subdirectory without --config.
// The discovery walk-up must find the file and apply it.
func TestSmoke_PR144_DiscoversConfig(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := pr144FixtureServer(t)

	tempDir := t.TempDir()
	cfgBody := "codeshape:\n  languages:\n    preferred: [Go]\n"
	if err := os.WriteFile(filepath.Join(tempDir, "pr-analyzer.yaml"), []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	subDir := filepath.Join(tempDir, "sub", "deeper")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cmd := exec.Command(bin, "sarahmaeve/signatory#144")
	cmd.Dir = subDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	if !strings.Contains(stdout.String(), "languages preferred: Go\n") {
		t.Errorf("discovered config not applied; output:\n%s", stdout.String())
	}
}

// TestSmoke_TrapdoorFixture exercises the agent-config-touched signal
// end-to-end against the fabricated Trapdoor-shape PR fixture. The
// fixture's file list contains exactly .cursorrules + CLAUDE.md (the
// canonical Trapdoor PR-against-legit-AI-project payload); the binary
// must render the agent-config bullet naming both paths in
// file-list order.
func TestSmoke_TrapdoorFixture(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := prTrapdoorFixtureServer(t)

	cmd := exec.Command(bin, "agentforge/copilot-toolkit#47")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("binary failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	out := stdout.String()
	wants := []string{
		"PR #47 secaudit-helper2026 https://github.com/agentforge/copilot-toolkit/pull/47\n",
		// 78 LOC → ceil(78/100)=1 glyph, default scale, no scale notice.
		"[+]\n",
		"adds: 78  deletes: 0  files: 2\n",
		"no tests touched\n",
		"no dependency manifest touched\n",
		// CLAUDE.md → Markdown; .cursorrules has no recognized extension.
		"languages: Markdown\n",
		// The signal under test: agent-config bullet naming both paths
		// in file-list order.
		"agent-config files touched: .cursorrules, CLAUDE.md\n",
		// Slice-4 engineer-profile bullet — the trapdoor fixture's
		// author_association is FIRST_TIME_CONTRIBUTOR (interesting,
		// not in the trusted allowlist), so the bullet must appear.
		"author association: FIRST_TIME_CONTRIBUTOR\n",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	if stderr.Len() != 0 {
		t.Errorf("expected empty stderr on success, got:\n%s", stderr.String())
	}
}

// TestSmoke_DoesNotInheritParentGitHubToken proves the binary's env is
// built from an explicit allowlist — a real GITHUB_TOKEN in the parent
// (this test runner's) environment must not be visible to the child.
//
// Two distinct checks: (1) cmd.Env contains exactly one GITHUB_TOKEN
// entry — guards against a regression to append(os.Environ(), ...);
// (2) the binary's outbound Authorization header is Bearer
// smoke-test-token and never contains the parent secret — proves the
// child resolves to the explicit value.
//
// Cannot run in parallel: t.Setenv mutates process-global state.
func TestSmoke_DoesNotInheritParentGitHubToken(t *testing.T) {
	const parentSecret = "PARENT-TOKEN-MUST-NOT-LEAK"
	t.Setenv("GITHUB_TOKEN", parentSecret)

	bin := buildBinary(t)

	var capturedAuth atomic.Value
	capturedAuth.Store("")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth.Store(r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusNotFound) // happy path not needed; we want the captured header
	}))
	t.Cleanup(srv.Close)

	cmd := exec.Command(bin, "o/r#1")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}

	// Property 1: cmd.Env contains exactly one GITHUB_TOKEN entry. With the
	// t.Setenv above guaranteeing the parent has GITHUB_TOKEN=parentSecret,
	// a regression to append(os.Environ(), ...) would push this to 2.
	tokenEntries := 0
	for _, e := range cmd.Env {
		if strings.HasPrefix(e, "GITHUB_TOKEN=") {
			tokenEntries++
		}
	}
	if tokenEntries != 1 {
		t.Errorf("cmd.Env has %d GITHUB_TOKEN entries; want exactly 1 (parent inheritance leak)", tokenEntries)
	}

	_ = cmd.Run() // we expect a non-zero exit because the fake server returns 404

	// Property 2: the binary used the explicit smoke-test-token and never
	// the parent's secret. A regression where authTransport falls back to
	// something other than os.Getenv("GITHUB_TOKEN") would surface here.
	auth, _ := capturedAuth.Load().(string)
	if auth != "Bearer smoke-test-token" {
		t.Errorf("Authorization header = %q, want %q", auth, "Bearer smoke-test-token")
	}
	if strings.Contains(auth, parentSecret) {
		t.Errorf("Authorization header leaked parent secret: %q", auth)
	}
}
