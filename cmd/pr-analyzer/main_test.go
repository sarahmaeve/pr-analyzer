package main

import (
	"bytes"
	"context"
	stdjson "encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	if args.Scan.PR != "o/r#1" {
		t.Errorf("Scan.PR = %q, want %q", args.Scan.PR, "o/r#1")
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

// TestParseTarget pins the dispatch surface: bare `owner/repo` is
// list mode (Number == 0), `owner/repo#N` and full PR URLs are
// single-PR mode (Number > 0). The function reuses the underlying
// parsePRRef path for single-PR forms — covered separately — so this
// test focuses on the discrimination plus the new bare-repo path.
func TestParseTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in       string
		want     target
		wantList bool
	}{
		// Bare repo — list mode.
		{"Kong/kong", target{Owner: "Kong", Repo: "kong", Number: 0}, true},
		{"sarahmaeve/signatory", target{Owner: "sarahmaeve", Repo: "signatory", Number: 0}, true},
		{"  Kong/kong  ", target{Owner: "Kong", Repo: "kong", Number: 0}, true},
		// owner/repo#N — single-PR mode.
		{"Kong/kong#14838", target{Owner: "Kong", Repo: "kong", Number: 14838}, false},
		// PR URL — single-PR mode.
		{"https://github.com/Kong/kong/pull/14838", target{Owner: "Kong", Repo: "kong", Number: 14838}, false},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			got, err := parseTarget(tc.in)
			if err != nil {
				t.Fatalf("parseTarget(%q) error: %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("parseTarget(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
			if got.IsList() != tc.wantList {
				t.Errorf("parseTarget(%q).IsList() = %v, want %v", tc.in, got.IsList(), tc.wantList)
			}
		})
	}
}

func TestParseTarget_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in              string
		wantErrContains string
	}{
		// Empty / unrecognized structure.
		{"", "usage:"},
		{"no-slash-at-all", "usage:"},
		// Bare repo URL: not supported in slice 5; users must use owner/repo or owner/repo#N.
		{"https://github.com/Kong/kong", "bare repo URL"},
		// Bad character in owner / repo for bare form.
		{"a b/c", "invalid owner"},
		{"o/r;evil", "invalid repo"},
		{"o/../r", "invalid repo"},
		// Trailing slash that would otherwise look like extra-segment repo.
		{"o/r/extra", "invalid repo"},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			_, err := parseTarget(tc.in)
			if err == nil {
				t.Fatalf("parseTarget(%q) returned nil, want error containing %q", tc.in, tc.wantErrContains)
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("parseTarget(%q) error = %v, want substring %q", tc.in, err, tc.wantErrContains)
			}
		})
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

	err := runScan(cliArgs{Scan: scanCmd{PR: "o/r#1"}}, io.Discard, io.Discard)
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

// TestRateLimitTransport_nextDelay_inRange pins the bounds: every
// produced delay lies in [min, max]. Tested with a non-default narrow
// range so a regression that, say, divides instead of adds would land
// outside [3, 7] visibly. Run enough iterations that a flaky off-by-
// one on the upper bound surfaces.
func TestRateLimitTransport_nextDelay_inRange(t *testing.T) {
	t.Parallel()

	rt := newRateLimitTransport(nil, 3*time.Millisecond, 7*time.Millisecond)
	for i := range 2000 {
		d := rt.nextDelay()
		if d < 3*time.Millisecond || d > 7*time.Millisecond {
			t.Fatalf("iteration %d: nextDelay() = %v, want in [3ms, 7ms]", i, d)
		}
	}
}

// TestRateLimitTransport_nextDelay_varies proves the delay is actually
// random — without this, a constant return of min would pass the
// "in-range" test silently. The chance of all 100 draws landing on the
// same of five possible millisecond buckets is (1/5)^99 ≈ 0, so a
// single distinct second value is enough evidence.
func TestRateLimitTransport_nextDelay_varies(t *testing.T) {
	t.Parallel()

	rt := newRateLimitTransport(nil, 3*time.Millisecond, 7*time.Millisecond)
	first := rt.nextDelay()
	for range 100 {
		if rt.nextDelay() != first {
			return
		}
	}
	t.Fatalf("nextDelay() returned %v on 101 consecutive calls — random source not engaged", first)
}

// TestRateLimitTransport_RoundTrip_appliesDelay proves the transport
// actually waits before the inner request goes out. Use a very short
// fixed bound (3ms == 3ms) so the test is deterministic-ish; assert
// elapsed >= bound minus a small slack for select-receive jitter.
func TestRateLimitTransport_RoundTrip_appliesDelay(t *testing.T) {
	t.Parallel()

	var sawRequest atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		sawRequest.Store(true)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	rt := newRateLimitTransport(http.DefaultTransport, 50*time.Millisecond, 50*time.Millisecond)
	client := &http.Client{Transport: rt}

	start := time.Now()
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	resp.Body.Close()
	elapsed := time.Since(start)

	if !sawRequest.Load() {
		t.Fatal("inner transport was not called")
	}
	if elapsed < 50*time.Millisecond {
		t.Errorf("elapsed = %v, want >= 50ms (delay not applied)", elapsed)
	}
}

// TestRateLimitTransport_RoundTrip_honorsContext proves an already-
// cancelled context aborts the sleep instead of waiting the full
// delay. Without this guard a user's Ctrl-C arrives only after the
// next sleep finishes — for 300-500ms that's tolerable, but the
// contract is "respect context", so pin it.
func TestRateLimitTransport_RoundTrip_honorsContext(t *testing.T) {
	t.Parallel()

	rt := newRateLimitTransport(http.DefaultTransport, 10*time.Second, 10*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://unused.invalid", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	start := time.Now()
	_, err = rt.RoundTrip(req)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("RoundTrip returned nil error for cancelled ctx")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
	if elapsed >= time.Second {
		t.Errorf("elapsed = %v; cancellation did not abort the 10s sleep", elapsed)
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
// org config on disk, binary picks it up, every slice-2 knob that
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

// TestSmoke_PR144_DiscoversUserLevelConfig exercises the slice-6
// fallback: when the CWD walk-up finds no pr-analyzer.yaml, the
// binary picks up ~/.config/pr-analyzer/pr-analyzer.yaml instead.
// The child env explicitly clears XDG_CONFIG_HOME so HOME's
// .config/pr-analyzer path is the one that wins.
func TestSmoke_PR144_DiscoversUserLevelConfig(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := pr144FixtureServer(t)

	// $HOME for the child; we'll plant a config under .config/pr-analyzer.
	homeDir := t.TempDir()
	userConfigDir := filepath.Join(homeDir, ".config", "pr-analyzer")
	if err := os.MkdirAll(userConfigDir, 0o755); err != nil {
		t.Fatalf("mkdir user config dir: %v", err)
	}
	cfgBody := "codeshape:\n  languages:\n    preferred: [Go]\n"
	if err := os.WriteFile(filepath.Join(userConfigDir, "pr-analyzer.yaml"), []byte(cfgBody), 0o600); err != nil {
		t.Fatalf("write user config: %v", err)
	}

	// CWD for the child — a separate tempdir with no walk-up hit, so
	// discovery falls through to the user-level path.
	cwd := t.TempDir()

	cmd := exec.Command(bin, "sarahmaeve/signatory#144")
	cmd.Dir = cwd
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + homeDir,
		// Explicit empty XDG_CONFIG_HOME: forces the HOME-based
		// fallback. A leaked XDG_CONFIG_HOME from the parent process
		// could otherwise point at the dev's real config.
		"XDG_CONFIG_HOME=",
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
		t.Errorf("user-level config not applied; output:\n%s", stdout.String())
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

// signatoryRepoListServer stands up an httptest server that serves
// the slice-5 list-mode fixture set: a /pulls?state=open response
// listing three open PRs (#144, #142, #141) and the per-PR detail +
// files endpoints for each. Used by TestSmoke_RepoList.
func signatoryRepoListServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "../../connectors/github/testdata/signatory_pulls_open.json")
	})
	// Per-PR routes. ServeMux dispatches /pulls/144 before /pulls because the
	// longer pattern is more specific.
	for _, n := range []int{144, 142, 141} {
		detailPath := fmt.Sprintf("/repos/sarahmaeve/signatory/pulls/%d", n)
		filesPath := detailPath + "/files"
		detailFixture := fmt.Sprintf("../../connectors/github/testdata/pr_%d.json", n)
		filesFixture := fmt.Sprintf("../../connectors/github/testdata/pr_%d_files.json", n)
		mux.HandleFunc(detailPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			http.ServeFile(w, r, detailFixture)
		})
		mux.HandleFunc(filesPath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			http.ServeFile(w, r, filesFixture)
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// TestSmoke_RepoList exercises slice-5 list mode end-to-end: bare
// repo arg → list endpoint → per-PR fetch loop → looped CLI text on
// stdout + analyses.json in --out dir. Three PRs land in the output,
// each as a distinct rendered block; the test asserts substrings
// unique to each PR so a regression that drops a PR or re-renders
// the same one three times fails loud.
//
// Slow by design: the rate-limit transport (300-500ms per request)
// applies in list mode — 7 calls = ~2-3s wall clock. Smoke tests run
// in parallel so this overlaps with the others.
func TestSmoke_RepoList(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := signatoryRepoListServer(t)
	outDir := t.TempDir()

	cmd := exec.Command(bin, "--out", outDir, "sarahmaeve/signatory")
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
	// One assertion per PR, anchored on a substring unique to that PR.
	// PR #144 carries the OWNER association (no author-association
	// bullet), 5631 adds; PR #142 is a small modification (author OWNER);
	// PR #141 is FIRST_TIME_CONTRIBUTOR (bullet visible).
	wants := []string{
		"PR #144 sarahmaeve https://github.com/sarahmaeve/signatory/pull/144\n",
		"adds: 5631  deletes: 220  files: 27\n",
		"PR #142 sarahmaeve https://github.com/sarahmaeve/signatory/pull/142\n",
		"adds: 24  deletes: 3  files: 1\n",
		"PR #141 drive-by-contributor https://github.com/sarahmaeve/signatory/pull/141\n",
		"adds: 2  deletes: 2  files: 1\n",
		"author association: FIRST_TIME_CONTRIBUTOR\n",
	}
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output:\n%s", want, out)
		}
	}

	// Progress lines appear on stderr; user piping stdout to a file
	// must still see something. One [N/M] line per PR.
	errStr := stderr.String()
	for _, want := range []string{"[1/3]", "[2/3]", "[3/3]"} {
		if !strings.Contains(errStr, want) {
			t.Errorf("stderr missing progress marker %q:\n%s", want, errStr)
		}
	}

	// analyses.json must exist in --out dir and decode to a valid
	// Envelope with all three PRs.
	jsonPath := filepath.Join(outDir, "analyses.json")
	body, err := os.ReadFile(jsonPath) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", jsonPath, err)
	}
	var env struct {
		SchemaVersion int            `json:"schema_version"`
		Repo          analyzer.PRRef `json:"repo"`
		Analyses      []struct {
			PR struct {
				Ref analyzer.PRRef `json:"ref"`
			} `json:"pr"`
		} `json:"analyses"`
	}
	if err := stdjson.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode envelope: %v\n%s", err, body)
	}
	if env.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", env.SchemaVersion)
	}
	if env.Repo.Owner != "sarahmaeve" || env.Repo.Repo != "signatory" {
		t.Errorf("repo = %+v, want sarahmaeve/signatory", env.Repo)
	}
	if len(env.Analyses) != 3 {
		t.Fatalf("len(analyses) = %d, want 3", len(env.Analyses))
	}
	gotNumbers := []int{env.Analyses[0].PR.Ref.Number, env.Analyses[1].PR.Ref.Number, env.Analyses[2].PR.Ref.Number}
	wantNumbers := []int{144, 142, 141}
	if !slices.Equal(gotNumbers, wantNumbers) {
		t.Errorf("analyses PR numbers = %v, want %v (preserves ListOpenPRs order)", gotNumbers, wantNumbers)
	}

	// index.html must exist alongside analyses.json and contain the
	// load-bearing markup tokens. Stable-contract details are tested
	// in render/html/html_test.go; here we only verify the wiring.
	htmlPath := filepath.Join(outDir, "index.html")
	htmlBody, err := os.ReadFile(htmlPath) //nolint:gosec // G304: test-controlled path
	if err != nil {
		t.Fatalf("read %s: %v", htmlPath, err)
	}
	htmlStr := string(htmlBody)
	htmlWants := []string{
		"<!doctype html>",
		`class="pra-pr"`,
		`href="https://github.com/sarahmaeve/signatory"`,
		`data-pra-pr-number="144"`,
		`data-pra-pr-number="141"`,
		// PR #141 in the fixture is FIRST_TIME_CONTRIBUTOR — orange pill.
		"FIRST_TIME_CONTRIBUTOR",
		"pra-pill-warning",
		// Inline data block is present.
		`<script type="application/json" id="pra-data">`,
	}
	for _, w := range htmlWants {
		if !strings.Contains(htmlStr, w) {
			t.Errorf("missing %q in index.html", w)
		}
	}
}

// TestSmoke_RenderHTML proves the render-html subcommand consumes
// an existing analyses.json and writes HTML to stdout. Drives the
// iteration loop where the user re-runs the renderer against a
// cached scan without re-fetching from GitHub.
func TestSmoke_RenderHTML(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)

	jsonBody := `{
  "schema_version": 1,
  "generated_at": "2026-05-25T10:00:00Z",
  "repo": {"owner": "atuinsh", "repo": "atuin", "number": 0},
  "analyses": [
    {
      "pr": {
        "ref": {"owner": "atuinsh", "repo": "atuin", "number": 42},
        "title": "Render-html smoke fixture",
        "author": "ellie",
        "url": "https://github.com/atuinsh/atuin/pull/42",
        "additions": 10,
        "deletions": 2,
        "changed_files": 3,
        "author_association": "MEMBER"
      },
      "code_shape": {
        "loc": {"additions": 10, "deletions": 2, "total": 12},
        "tests_touched": true,
        "languages": ["Rust"]
      },
      "engineer_profile": {"author_association": "MEMBER"}
    }
  ]
}`
	jsonPath := filepath.Join(t.TempDir(), "analyses.json")
	if err := os.WriteFile(jsonPath, []byte(jsonBody), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cmd := exec.Command(bin, "render-html", jsonPath)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("render-html failed: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	wants := []string{
		"<!doctype html>",
		"atuinsh/atuin",
		`href="https://github.com/atuinsh/atuin/pull/42"`,
		`href="https://github.com/ellie"`,
		"Render-html smoke fixture",
		`<script type="application/json" id="pra-data">`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in render-html output", w)
		}
	}
}

// TestSmoke_Inspect runs the binary end-to-end via the inspect
// subcommand: scan three fixture PRs to disk, then re-invoke
// `pr-analyzer inspect <analyses.json>` and assert the summary
// renders the load-bearing rows. Two-stage so the inspect path
// exercises the same JSON file a user would consume.
func TestSmoke_Inspect(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)
	srv := signatoryRepoListServer(t)
	outDir := t.TempDir()

	// Stage 1: produce the analyses.json artifact.
	scan := exec.Command(bin, "--out", outDir, "sarahmaeve/signatory")
	scan.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_TOKEN=smoke-test-token",
		"GITHUB_API_BASE_URL=" + srv.URL,
	}
	if out, err := scan.CombinedOutput(); err != nil {
		t.Fatalf("scan stage failed: %v\n%s", err, out)
	}

	// Stage 2: inspect the produced artifact.
	inspect := exec.Command(bin, "inspect", filepath.Join(outDir, "analyses.json"))
	var stdout, stderr bytes.Buffer
	inspect.Stdout = &stdout
	inspect.Stderr = &stderr
	if err := inspect.Run(); err != nil {
		t.Fatalf("inspect stage failed: %v\nstderr:\n%s", err, stderr.String())
	}

	out := stdout.String()
	wants := []string{
		// Header: repo, count, schema version.
		"sarahmaeve/signatory",
		"3 open PRs",
		"schema v1",
		// Section headers — proves every writeX function ran.
		"Author association",
		"Languages",
		"Lines of code",
		"Tests touched",
		"Agent-config files touched",
		// Specific row content from the fixture data:
		"FIRST_TIME_CONTRIBUTOR", // PR #141's author bucket
		"Go",                     // detected language (PR #144, #142)
		"5851",                   // PR #144's LOC total (5631+220)
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in inspect output:\n%s", w, out)
		}
	}
}

// TestSmoke_RepoList_RequiresGitHubToken proves the list-mode token
// gate fires before any network activity. With no GITHUB_TOKEN in the
// child's env, the binary must exit non-zero with stderr naming the
// env var — and the fixture server must observe zero hits, since
// hitting GitHub anonymously for 80+ PRs would burn the public-IP
// rate limit instantly.
func TestSmoke_RepoList_RequiresGitHubToken(t *testing.T) {
	t.Parallel()

	bin := buildBinary(t)

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)

	cmd := exec.Command(bin, "sarahmaeve/signatory")
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		"GITHUB_API_BASE_URL=" + srv.URL,
		// Deliberately omit GITHUB_TOKEN.
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		t.Fatal("binary succeeded without GITHUB_TOKEN; want non-zero exit")
	}
	if !strings.Contains(stderr.String(), "GITHUB_TOKEN") {
		t.Errorf("stderr does not mention GITHUB_TOKEN: %s", stderr.String())
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("fixture server observed %d hits; want 0 (token check must fire before any network)", n)
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
