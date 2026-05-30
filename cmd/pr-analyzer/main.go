// Command pr-analyzer analyzes a GitHub pull request and renders its
// Code Shape signals as the bar + bullets format described in
// design/PROTO.md.
package main

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/alecthomas/kong"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/configfile"
	"github.com/sarahmaeve/pr-analyzer/connectors/github"
	"github.com/sarahmaeve/pr-analyzer/internal/credentials"
	"github.com/sarahmaeve/pr-analyzer/render/cli"
	rhtml "github.com/sarahmaeve/pr-analyzer/render/html"
	rjson "github.com/sarahmaeve/pr-analyzer/render/json"
)

// Rate-limit bounds for list-mode HTTP traffic. 300-500ms randomized
// keeps us well under GitHub's authed 5000 req/hour budget even on
// repos with 100+ open PRs and adds enough jitter that we don't sync
// up with bursts from other clients.
const (
	listModeRateLimitMin = 300 * time.Millisecond
	listModeRateLimitMax = 500 * time.Millisecond
	// listModeTimeout is generous enough for ~100 PRs * 3 calls/PR at
	// 500ms each (~150s) plus headroom. Larger scans should chunk.
	listModeTimeout   = 10 * time.Minute
	singleModeTimeout = 60 * time.Second
)

const usage = "usage: pr-analyzer <owner/repo | owner/repo#number | https://github.com/owner/repo/pull/N>"

// cli is the declarative CLI surface. Kong fills it in from os.Args
// and validates flag types (existing files / dirs) before any
// subcommand handler runs.
//
// Subcommands:
//   - Scan (default): analyze a PR or all open PRs in a repo. Marked
//     default:"withargs" so the slice-1..5 invocation form
//     `pr-analyzer owner/repo[#N]` continues to work without the user
//     having to type the verb.
//   - Inspect: print a summary of a previously-generated analyses.json.
//     A second binary would have worked, but a subcommand keeps
//     packaging to one artifact and leaves room for future verbs
//     (diff, stats, etc.) without further restructuring.
type cliArgs struct {
	Config        string `short:"c" type:"existingfile" help:"Path to org config file (overrides walk-up + XDG / HOME discovery)."`
	LocalCloneDir string `name:"local-clone-dir" type:"existingdir" help:"Local checkout of the PR's repository. Defaults to CWD when unset."`

	Scan       scanCmd       `cmd:"" default:"withargs" help:"Analyze a PR or all open PRs in a repo (default)."`
	Inspect    inspectCmd    `cmd:"" help:"Print a summary of a previously-generated analyses.json."`
	RenderHTML renderHTMLCmd `cmd:"render-html" help:"Render an HTML report to stdout from a previously-generated analyses.json."`
}

type scanCmd struct {
	Out string `name:"out" type:"path" default:"." help:"Output directory for list-mode artifacts (analyses.json). Created if missing. Ignored in single-PR mode."`
	PR  string `arg:"" name:"pr-ref" help:"PR target: owner/repo (list mode), owner/repo#number, or full GitHub PR URL."`
}

type inspectCmd struct {
	Path string `arg:"" type:"existingfile" name:"analyses-json" help:"Path to analyses.json produced by a previous scan."`
}

type renderHTMLCmd struct {
	Path string `arg:"" type:"existingfile" name:"analyses-json" help:"Path to analyses.json produced by a previous scan."`
}

func main() {
	var c cliArgs
	ctx := kong.Parse(&c,
		kong.Name("pr-analyzer"),
		kong.Description("Analyzes GitHub pull requests and emits signal reports."),
	)
	var err error
	switch ctx.Command() {
	case "inspect <analyses-json>":
		err = runInspect(c.Inspect, os.Stdout)
	case "render-html <analyses-json>":
		err = runRenderHTML(c.RenderHTML, os.Stdout)
	default:
		err = runScan(c, os.Stdout, os.Stderr)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func runScan(c cliArgs, stdout, stderr io.Writer) error {
	tgt, err := parseTarget(c.Scan.PR)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get working directory: %w", err)
	}

	cfg, warnings, err := loadConfig(c.Config, cwd)
	if err != nil {
		return err
	}
	for _, w := range warnings {
		if w.Line > 0 {
			fmt.Fprintf(stderr, "warning (line %d): %s\n", w.Line, w.Message)
		} else {
			fmt.Fprintf(stderr, "warning: %s\n", w.Message)
		}
	}

	cfg.LocalCloneDir = resolveLocalCloneDir(c.LocalCloneDir, cfg.LocalCloneDir, cwd)

	baseURL := os.Getenv("GITHUB_API_BASE_URL")
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}

	// List mode hits the API many times in a single invocation; without a
	// token GitHub's anonymous 60-req/hour budget can't service even a
	// small repo. Require the token before any network activity so the
	// user gets a clear error instead of a confusing mid-scan 403.
	if tgt.IsList() {
		if err := credentials.GitHub.Require(); err != nil {
			return err
		}
	}

	httpClient := newHTTPClient(tgt.IsList())
	src := github.NewClient(httpClient, baseURL)

	timeout := singleModeTimeout
	if tgt.IsList() {
		timeout = listModeTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if tgt.IsList() {
		return runList(ctx, src, tgt, cfg, c.Scan.Out, stdout, stderr)
	}
	return runSingle(ctx, src, tgt.Ref(), cfg, stdout)
}

// runSingle preserves slice-1..4 behavior: one Analyze call, one CLI
// render to stdout.
func runSingle(ctx context.Context, src analyzer.PRSource, ref analyzer.PRRef, cfg analyzer.Config, stdout io.Writer) error {
	analysis, err := analyzer.Analyze(ctx, src, ref, analyzer.WithConfig(cfg))
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, cli.Render(analysis, cfg.Render))
	return err
}

// runList lists open PRs for tgt's repo, analyzes each, and emits
// two artifacts: per-PR CLI text to stdout for immediate human
// feedback, and analyses.json in outDir for machine consumption.
// Per-PR failures are logged to stderr and the scan continues — a
// single flaky upstream response should not abort a 100-PR report.
func runList(ctx context.Context, src analyzer.PRSource, tgt target, cfg analyzer.Config, outDir string, stdout, stderr io.Writer) error {
	refs, err := src.ListOpenPRs(ctx, tgt.Owner, tgt.Repo)
	if err != nil {
		return fmt.Errorf("list open PRs for %s/%s: %w", tgt.Owner, tgt.Repo, err)
	}
	fmt.Fprintf(stderr, "scanning %d open PRs in %s/%s\n", len(refs), tgt.Owner, tgt.Repo)

	analyses := make([]analyzer.Analysis, 0, len(refs))
	for i, ref := range refs {
		// Top-of-loop cancellation check: a timeout or Ctrl+C reaches us as
		// ctx.Err(), and without this every remaining PR would be logged
		// as a per-PR "skipping" warning — drowning the real cause and
		// silently writing a partial report. One clean abort instead.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("scan aborted: %w", err)
		}
		fmt.Fprintf(stderr, "[%d/%d] PR #%d\n", i+1, len(refs), ref.Number)
		analysis, err := analyzer.Analyze(ctx, src, ref, analyzer.WithConfig(cfg))
		if err != nil {
			fmt.Fprintf(stderr, "warning: skipping PR #%d: %v\n", ref.Number, err)
			continue
		}
		analyses = append(analyses, analysis)
		if len(analyses) > 1 {
			fmt.Fprintln(stdout)
		}
		if _, err := io.WriteString(stdout, cli.Render(analysis, cfg.Render)); err != nil {
			return err
		}
	}

	jsonPath, htmlPath, err := writeReportArtifacts(analyses, tgt, outDir)
	if err != nil {
		return err
	}
	fmt.Fprintf(stderr, "wrote %s and %s (%d PRs)\n", jsonPath, htmlPath, len(analyses))
	return nil
}

// writeReportArtifacts renders the analyses envelope once, writes
// analyses.json + index.html into outDir from that single source,
// and returns both paths so the caller can surface them on stderr.
// Both artifacts derive from the same in-memory envelope so the
// inlined JSON in index.html and the sibling analyses.json file
// cannot drift.
func writeReportArtifacts(analyses []analyzer.Analysis, tgt target, outDir string) (jsonPath, htmlPath string, err error) {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", "", fmt.Errorf("create out dir %s: %w", outDir, err)
	}
	repo := analyzer.PRRef{Owner: tgt.Owner, Repo: tgt.Repo}
	now := time.Now().UTC()
	env := rjson.Envelope{
		SchemaVersion: rjson.SchemaVersion,
		GeneratedAt:   now,
		Repo:          repo,
		Analyses:      analyses,
	}

	jsonBody, err := rjson.Render(analyses, repo, now)
	if err != nil {
		return "", "", fmt.Errorf("render analyses.json: %w", err)
	}
	jsonPath = filepath.Join(outDir, "analyses.json")
	if err := os.WriteFile(jsonPath, jsonBody, 0o644); err != nil { //nolint:gosec // G306: PR-scan artifact is intended to be world-readable
		return "", "", fmt.Errorf("write %s: %w", jsonPath, err)
	}

	htmlBody, err := rhtml.Render(env)
	if err != nil {
		return "", "", fmt.Errorf("render index.html: %w", err)
	}
	htmlPath = filepath.Join(outDir, "index.html")
	if err := os.WriteFile(htmlPath, []byte(htmlBody), 0o644); err != nil { //nolint:gosec // G306: PR-scan artifact is intended to be world-readable
		return "", "", fmt.Errorf("write %s: %w", htmlPath, err)
	}
	return jsonPath, htmlPath, nil
}

// newHTTPClient builds the HTTP client used by the GitHub connector.
// In list mode the transport stack is wrapped with rateLimitTransport
// to keep us under the upstream's rate-limit budget; single-PR mode
// only makes two calls, so no throttling is needed.
func newHTTPClient(listMode bool) *http.Client {
	transport := http.DefaultTransport
	var rt http.RoundTripper = &authTransport{
		token: credentials.GitHub.Token(),
		base:  transport,
	}
	if listMode {
		rt = newRateLimitTransport(rt, listModeRateLimitMin, listModeRateLimitMax)
	}
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: rt,
	}
}

// loadConfig returns the org config and any non-fatal warnings.
// When the user passed --config, the path must exist (fatal
// otherwise); otherwise we walk up from startDir looking for
// pr-analyzer.yaml, then fall through to $XDG_CONFIG_HOME/pr-analyzer
// and $HOME/.config/pr-analyzer per slice 6, accepting a miss at
// every level silently.
func loadConfig(explicitPath, startDir string) (analyzer.Config, []configfile.Warning, error) {
	if explicitPath != "" {
		return configfile.Load(explicitPath)
	}
	cfg, _, warnings, err := configfile.Discover(startDir)
	return cfg, warnings, err
}

// resolveLocalCloneDir applies slice-3's precedence ladder. The flag
// value (if present) wins, resolving against CWD when relative; the
// YAML value (if present) is used as-is, since the loader has already
// resolved it against the config-file directory; otherwise CWD is the
// default.
func resolveLocalCloneDir(flagValue, yamlValue, cwd string) string {
	if flagValue != "" {
		if filepath.IsAbs(flagValue) {
			return flagValue
		}
		return filepath.Join(cwd, flagValue)
	}
	if yamlValue != "" {
		return yamlValue
	}
	return cwd
}

// rateLimitTransport sleeps a uniform-random duration in
// [minDelay, maxDelay] before each request, so a list-mode loop does
// not hammer the upstream API. The transport is intentionally
// stateless beyond the bounds — no token-bucket, no last-call
// tracking — because pr-analyzer's traffic pattern is
// sequential-and-slow, not bursty. math/rand/v2's package-level
// functions are concurrency-safe, so no mutex is needed even if
// Go's http.Client decides to call us from multiple goroutines.
type rateLimitTransport struct {
	base     http.RoundTripper
	minDelay time.Duration
	maxDelay time.Duration
}

func newRateLimitTransport(base http.RoundTripper, minDelay, maxDelay time.Duration) *rateLimitTransport {
	return &rateLimitTransport{base: base, minDelay: minDelay, maxDelay: maxDelay}
}

// nextDelay picks a uniform-random duration in [minDelay, maxDelay].
// Inclusive on both ends so the documented bounds are honored
// literally; a degenerate range (max <= min) collapses to minDelay.
func (t *rateLimitTransport) nextDelay() time.Duration {
	if t.maxDelay <= t.minDelay {
		return t.minDelay
	}
	spread := t.maxDelay - t.minDelay
	return t.minDelay + time.Duration(rand.Int64N(int64(spread)+1))
}

func (t *rateLimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case <-time.After(t.nextDelay()):
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return t.base.RoundTrip(req)
}

type authTransport struct {
	token string
	base  http.RoundTripper
}

// RoundTrip implements http.RoundTripper. Per the interface contract, it
// must not mutate the request it is given — mutation can leak the
// Authorization header to other observers (e.g. errors that wrap
// *http.Request on redirect paths). We clone the request before adding
// the Bearer token.
func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token == "" {
		return t.base.RoundTrip(req)
	}
	r2 := req.Clone(req.Context())
	r2.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(r2)
}

// loopbackHosts permits the GITHUB_API_BASE_URL override for tests that
// run an httptest.Server. Any other host requires the canonical GitHub
// API endpoint over HTTPS.
var loopbackHosts = map[string]struct{}{
	"127.0.0.1": {},
	"::1":       {},
	"localhost": {},
}

// validateBaseURL ensures the override is safe to send the Bearer token
// to. An attacker who can set this env var (CI misconfiguration, confused
// deputy, etc.) must not be able to redirect authenticated requests at an
// arbitrary host.
func validateBaseURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid GITHUB_API_BASE_URL %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("GITHUB_API_BASE_URL must use http or https; got %q in %q", u.Scheme, raw)
	}
	host := u.Hostname()
	if u.Scheme == "https" && host == "api.github.com" {
		return nil
	}
	if _, ok := loopbackHosts[host]; ok {
		return nil
	}
	return fmt.Errorf("GITHUB_API_BASE_URL must be https://api.github.com or a loopback URL (127.0.0.1, localhost, ::1); got %q", raw)
}

var (
	prURLRegexp        = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)/?$`)
	repoURLRegexp      = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/?$`)
	ownerRepoCharRegex = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// validateOwnerRepo defends against path-injection, header-injection,
// and silly-but-possible values smuggled into URLs. The character set
// is a safe subset of what GitHub actually accepts. Shared by single-
// PR and bare-repo parsing.
func validateOwnerRepo(owner, repo string) error {
	if !ownerRepoCharRegex.MatchString(owner) {
		return fmt.Errorf("invalid owner %q (allowed: alphanumeric, dot, underscore, hyphen)", owner)
	}
	if !ownerRepoCharRegex.MatchString(repo) {
		return fmt.Errorf("invalid repo %q (allowed: alphanumeric, dot, underscore, hyphen)", repo)
	}
	return nil
}

func validateRef(ref analyzer.PRRef) error {
	if err := validateOwnerRepo(ref.Owner, ref.Repo); err != nil {
		return err
	}
	if ref.Number <= 0 {
		return fmt.Errorf("PR number must be positive; got %d", ref.Number)
	}
	return nil
}

// target is the parsed pr-ref argument: either a specific PR or a
// bare-repo "list all open PRs" request. Number == 0 discriminates
// list mode; positive Number is single-PR mode.
type target struct {
	Owner  string
	Repo   string
	Number int
}

func (t target) IsList() bool { return t.Number == 0 }

func (t target) Ref() analyzer.PRRef {
	return analyzer.PRRef{Owner: t.Owner, Repo: t.Repo, Number: t.Number}
}

// parseTarget recognizes:
//   - owner/repo                                  → list mode
//   - owner/repo#N                                → single PR
//   - https://github.com/owner/repo/pull/N        → single PR
//
// A bare repo URL (https://github.com/owner/repo) is detected and
// rejected explicitly so the user gets "use owner/repo or owner/repo#N"
// instead of the generic usage line. Other shapes fall through to the
// usage error.
func parseTarget(s string) (target, error) {
	s = strings.TrimSpace(s)
	if strings.Contains(s, "#") || strings.Contains(s, "/pull/") {
		ref, err := parsePRRef(s)
		if err != nil {
			return target{}, err
		}
		return target{Owner: ref.Owner, Repo: ref.Repo, Number: ref.Number}, nil
	}
	if repoURLRegexp.MatchString(s) {
		return target{}, fmt.Errorf("bare repo URLs are not yet supported; use owner/repo or owner/repo#N\ngot: %q", s)
	}
	return parseRepoRef(s)
}

func parseRepoRef(s string) (target, error) {
	slash := strings.Index(s, "/")
	if slash < 0 || slash == len(s)-1 {
		return target{}, fmt.Errorf("%s\ngot: %q", usage, s)
	}
	t := target{Owner: s[:slash], Repo: s[slash+1:]}
	if err := validateOwnerRepo(t.Owner, t.Repo); err != nil {
		return target{}, err
	}
	return t, nil
}

func parsePRRef(s string) (analyzer.PRRef, error) {
	s = strings.TrimSpace(s)
	if m := prURLRegexp.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return analyzer.PRRef{}, fmt.Errorf("invalid PR number in URL: %w", err)
		}
		ref := analyzer.PRRef{Owner: m[1], Repo: m[2], Number: n}
		if err := validateRef(ref); err != nil {
			return analyzer.PRRef{}, err
		}
		return ref, nil
	}
	hash := strings.Index(s, "#")
	slash := strings.Index(s, "/")
	if hash < 0 || slash < 0 || slash > hash {
		return analyzer.PRRef{}, fmt.Errorf("%s\ngot: %q", usage, s)
	}
	n, err := strconv.Atoi(s[hash+1:])
	if err != nil {
		return analyzer.PRRef{}, fmt.Errorf("invalid PR number after '#': %w", err)
	}
	ref := analyzer.PRRef{
		Owner:  s[:slash],
		Repo:   s[slash+1 : hash],
		Number: n,
	}
	if err := validateRef(ref); err != nil {
		return analyzer.PRRef{}, err
	}
	return ref, nil
}
