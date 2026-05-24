// Command pr-analyzer analyzes a GitHub pull request and renders its
// Code Shape signals as the bar + bullets format described in
// design/PROTO.md.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/connectors/github"
	"github.com/sarahmaeve/pr-analyzer/render/cli"
)

const usage = "usage: pr-analyzer <owner/repo#number | https://github.com/owner/repo/pull/N>"

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return errors.New(usage)
	}
	ref, err := parsePRRef(args[0])
	if err != nil {
		return err
	}

	baseURL := os.Getenv("GITHUB_API_BASE_URL")
	if err := validateBaseURL(baseURL); err != nil {
		return err
	}

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &authTransport{
			token: os.Getenv("GITHUB_TOKEN"),
			base:  http.DefaultTransport,
		},
	}

	src := github.NewClient(httpClient, baseURL)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	analysis, err := analyzer.Analyze(ctx, src, ref)
	if err != nil {
		return err
	}

	_, err = io.WriteString(stdout, cli.Render(analysis))
	return err
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
	ownerRepoCharRegex = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// validateRef defends against path-injection, header-injection, and
// silly-but-possible values smuggled into URLs. The character set is a
// safe subset of what GitHub actually accepts.
func validateRef(ref analyzer.PRRef) error {
	if !ownerRepoCharRegex.MatchString(ref.Owner) {
		return fmt.Errorf("invalid owner %q (allowed: alphanumeric, dot, underscore, hyphen)", ref.Owner)
	}
	if !ownerRepoCharRegex.MatchString(ref.Repo) {
		return fmt.Errorf("invalid repo %q (allowed: alphanumeric, dot, underscore, hyphen)", ref.Repo)
	}
	if ref.Number <= 0 {
		return fmt.Errorf("PR number must be positive; got %d", ref.Number)
	}
	return nil
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
