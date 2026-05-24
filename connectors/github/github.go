// Package github provides a PRSource implementation against GitHub's
// REST API. The client accepts a caller-supplied *http.Client so that
// authentication (e.g. a Bearer token transport) is configured outside
// the library.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

const defaultBaseURL = "https://api.github.com"

type Client struct {
	httpClient *http.Client
	baseURL    string
}

func NewClient(httpClient *http.Client, baseURL string) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    strings.TrimRight(baseURL, "/"),
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
	Number       int            `json:"number"`
	Title        string         `json:"title"`
	HTMLURL      string         `json:"html_url"`
	State        string         `json:"state"`
	Draft        bool           `json:"draft"`
	User         userPayload    `json:"user"`
	Base         refPayload     `json:"base"`
	Head         refPayload     `json:"head"`
	Additions    int            `json:"additions"`
	Deletions    int            `json:"deletions"`
	ChangedFiles int            `json:"changed_files"`
	Labels       []labelPayload `json:"labels"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type userPayload struct {
	Login string `json:"login"`
}

type refPayload struct {
	Ref string `json:"ref"`
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
		Ref:          ref,
		Title:        p.Title,
		Author:       p.User.Login,
		URL:          p.HTMLURL,
		State:        p.State,
		Draft:        p.Draft,
		BaseRef:      p.Base.Ref,
		HeadRef:      p.Head.Ref,
		Additions:    p.Additions,
		Deletions:    p.Deletions,
		ChangedFiles: p.ChangedFiles,
		Labels:       labels,
		CreatedAt:    p.CreatedAt,
		UpdatedAt:    p.UpdatedAt,
	}, nil
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

	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}

	return parseNextLink(resp.Header.Get("Link")), nil
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
