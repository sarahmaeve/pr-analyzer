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

	httpClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &authTransport{
			token: os.Getenv("GITHUB_TOKEN"),
			base:  http.DefaultTransport,
		},
	}

	src := github.NewClient(httpClient, os.Getenv("GITHUB_API_BASE_URL"))

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

func (t *authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.token != "" {
		req.Header.Set("Authorization", "Bearer "+t.token)
	}
	return t.base.RoundTrip(req)
}

var prURLRegexp = regexp.MustCompile(`^https?://github\.com/([^/]+)/([^/]+)/pull/(\d+)/?$`)

func parsePRRef(s string) (analyzer.PRRef, error) {
	s = strings.TrimSpace(s)
	if m := prURLRegexp.FindStringSubmatch(s); m != nil {
		n, err := strconv.Atoi(m[3])
		if err != nil {
			return analyzer.PRRef{}, fmt.Errorf("invalid PR number in URL: %w", err)
		}
		return analyzer.PRRef{Owner: m[1], Repo: m[2], Number: n}, nil
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
	return analyzer.PRRef{
		Owner:  s[:slash],
		Repo:   s[slash+1 : hash],
		Number: n,
	}, nil
}
