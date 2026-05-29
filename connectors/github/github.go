// Package github provides a PRSource implementation against GitHub's
// REST API. The client accepts a caller-supplied *http.Client so that
// authentication (e.g. a Bearer token transport) is configured outside
// the library.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

const (
	defaultBaseURL          = "https://api.github.com"
	defaultMaxResponseBytes = int64(32 << 20) // 32 MiB
)

type Client struct {
	httpClient       *http.Client
	baseURL          string
	maxResponseBytes int64
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		httpClient:       httpClient,
		baseURL:          strings.TrimRight(baseURL, "/"),
		maxResponseBytes: defaultMaxResponseBytes,
	}
}

func (c *Client) FetchPR(ctx context.Context, ref analyzer.PRRef) (analyzer.PR, error) {
	pr, err := c.fetchPRDetail(ctx, ref)
	if err != nil {
		return analyzer.PR{}, err
	}
	files, err := c.fetchAllFiles(ctx, ref)
	if err != nil {
		return analyzer.PR{}, err
	}
	pr.Files = files
	return pr, nil
}

type prPayload struct {
	Number            int            `json:"number"`
	Title             string         `json:"title"`
	HTMLURL           string         `json:"html_url"`
	State             string         `json:"state"`
	Draft             bool           `json:"draft"`
	User              userPayload    `json:"user"`
	Base              refPayload     `json:"base"`
	Head              refPayload     `json:"head"`
	Additions         int            `json:"additions"`
	Deletions         int            `json:"deletions"`
	ChangedFiles      int            `json:"changed_files"`
	Labels            []labelPayload `json:"labels"`
	AuthorAssociation string         `json:"author_association"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type userPayload struct {
	Login string `json:"login"`
}

type refPayload struct {
	Ref string `json:"ref"`
	SHA string `json:"sha"`
}

type labelPayload struct {
	Name string `json:"name"`
}

type filePayload struct {
	Filename  string `json:"filename"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

func (c *Client) fetchPRDetail(ctx context.Context, ref analyzer.PRRef) (analyzer.PR, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", c.baseURL, ref.Owner, ref.Repo, ref.Number)
	var p prPayload
	if _, err := c.getJSON(ctx, url, &p); err != nil {
		return analyzer.PR{}, fmt.Errorf("fetch PR detail: %w", err)
	}

	labels := make([]string, len(p.Labels))
	for i, l := range p.Labels {
		labels[i] = l.Name
	}

	return analyzer.PR{
		Ref:               ref,
		Title:             p.Title,
		Author:            p.User.Login,
		URL:               p.HTMLURL,
		State:             p.State,
		Draft:             p.Draft,
		BaseRef:           p.Base.Ref,
		HeadRef:           p.Head.Ref,
		BaseSHA:           p.Base.SHA,
		HeadSHA:           p.Head.SHA,
		Additions:         p.Additions,
		Deletions:         p.Deletions,
		ChangedFiles:      p.ChangedFiles,
		Labels:            labels,
		AuthorAssociation: p.AuthorAssociation,
		CreatedAt:         p.CreatedAt,
		UpdatedAt:         p.UpdatedAt,
	}, nil
}

// ListOpenPRs returns the open PRs for owner/repo, walking the Link-
// header pagination to completion. The listing endpoint omits
// additions / deletions / changed_files / file lists; callers that
// need those must follow up with FetchPR per ref. Only `number` is
// load-bearing here — the other fields are decoded leniently for
// debuggability but not surfaced.
func (c *Client) ListOpenPRs(ctx context.Context, owner, repo string) ([]analyzer.PRRef, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls?state=open&per_page=100", c.baseURL, owner, repo)
	var refs []analyzer.PRRef
	for url != "" {
		var page []prListItem
		next, err := c.getJSON(ctx, url, &page)
		if err != nil {
			return nil, fmt.Errorf("list open PRs for %s/%s: %w", owner, repo, err)
		}
		for _, item := range page {
			refs = append(refs, analyzer.PRRef{Owner: owner, Repo: repo, Number: item.Number})
		}
		url = next
	}
	return refs, nil
}

// prListItem is the trimmed shape of one element in the
// /repos/{o}/{r}/pulls response. The listing endpoint returns the
// same outer shape as PR detail, but additions / deletions /
// changed_files / files are absent — the connector intentionally
// declines to model the rest of the response so that future readers
// don't mistake the listing for a "PR detail + extras" call.
type prListItem struct {
	Number int `json:"number"`
}

func (c *Client) fetchAllFiles(ctx context.Context, ref analyzer.PRRef) ([]analyzer.PRFile, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d/files?per_page=100", c.baseURL, ref.Owner, ref.Repo, ref.Number)
	var all []analyzer.PRFile
	for url != "" {
		var page []filePayload
		next, err := c.getJSON(ctx, url, &page)
		if err != nil {
			return nil, fmt.Errorf("fetch PR files: %w", err)
		}
		for _, f := range page {
			all = append(all, analyzer.PRFile{
				Path:      f.Filename,
				Status:    f.Status,
				Additions: f.Additions,
				Deletions: f.Deletions,
			})
		}
		url = next
	}
	return all, nil
}

// getJSON GETs url, decodes the JSON body into dest, and returns the
// URL of the next page from the Link header (empty if no next page).
func (c *Client) getJSON(ctx context.Context, url string, dest any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	// Cap body reads so a hostile or compromised server cannot OOM the process.
	// We allow one extra byte so we can detect when the body was exactly at or
	// beyond the limit.
	limited := &io.LimitedReader{R: resp.Body, N: c.maxResponseBytes + 1}
	if err := json.NewDecoder(limited).Decode(dest); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if limited.N <= 0 {
		return "", fmt.Errorf("response body exceeds %d-byte limit", c.maxResponseBytes)
	}

	next := parseNextLink(resp.Header.Get("Link"))
	if next != "" {
		if err := c.validateSameOrigin(next); err != nil {
			return "", err
		}
	}
	return next, nil
}

// validateSameOrigin ensures rawURL has the same scheme and host as the
// client's configured base URL. This blocks a hostile server from
// redirecting paginated requests — and the Authorization header that the
// caller's transport may attach — to an attacker-controlled host.
func (c *Client) validateSameOrigin(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid next-link URL %q: %w", rawURL, err)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return fmt.Errorf("invalid client base URL %q: %w", c.baseURL, err)
	}
	if u.Scheme != base.Scheme || u.Host != base.Host {
		return fmt.Errorf("next-link origin %s://%s does not match base %s://%s",
			u.Scheme, u.Host, base.Scheme, base.Host)
	}
	return nil
}

func parseNextLink(header string) string {
	if header == "" {
		return ""
	}
	for part := range strings.SplitSeq(header, ",") {
		part = strings.TrimSpace(part)
		if !strings.HasPrefix(part, "<") {
			continue
		}
		i := strings.Index(part, ">")
		if i < 0 {
			continue
		}
		url := part[1:i]
		attrs := part[i+1:]
		if strings.Contains(attrs, `rel="next"`) {
			return url
		}
	}
	return ""
}
