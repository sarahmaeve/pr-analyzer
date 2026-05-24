package github_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/connectors/github"
)

func TestClient_FetchPR_PR144Fixture(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls/144", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "testdata/pr_144.json")
	})
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls/144/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "testdata/pr_144_files.json")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	pr, err := c.FetchPR(context.Background(), analyzer.PRRef{
		Owner: "sarahmaeve", Repo: "signatory", Number: 144,
	})
	if err != nil {
		t.Fatalf("FetchPR error: %v", err)
	}

	if pr.Ref.Number != 144 {
		t.Errorf("Ref.Number = %d, want 144", pr.Ref.Number)
	}
	if pr.Title != "AST signal collection for NPM and node" {
		t.Errorf("Title = %q, want AST signal collection...", pr.Title)
	}
	if pr.Author != "sarahmaeve" {
		t.Errorf("Author = %q, want sarahmaeve", pr.Author)
	}
	if pr.URL != "https://github.com/sarahmaeve/signatory/pull/144" {
		t.Errorf("URL = %q, want https://github.com/sarahmaeve/signatory/pull/144", pr.URL)
	}
	if pr.State != "open" {
		t.Errorf("State = %q, want open", pr.State)
	}
	if pr.Draft {
		t.Error("Draft = true, want false")
	}
	if pr.BaseRef != "main" {
		t.Errorf("BaseRef = %q, want main", pr.BaseRef)
	}
	if pr.HeadRef != "npm-ast" {
		t.Errorf("HeadRef = %q, want npm-ast", pr.HeadRef)
	}
	if pr.Additions != 5631 {
		t.Errorf("Additions = %d, want 5631", pr.Additions)
	}
	if pr.Deletions != 220 {
		t.Errorf("Deletions = %d, want 220", pr.Deletions)
	}
	if pr.ChangedFiles != 27 {
		t.Errorf("ChangedFiles = %d, want 27", pr.ChangedFiles)
	}
	if len(pr.Files) != 27 {
		t.Fatalf("len(Files) = %d, want 27", len(pr.Files))
	}
	first := pr.Files[0]
	if first.Path != "cmd/signatory/collectors.go" {
		t.Errorf("Files[0].Path = %q, want cmd/signatory/collectors.go", first.Path)
	}
	if first.Status != "modified" {
		t.Errorf("Files[0].Status = %q, want modified", first.Status)
	}
	if first.Additions != 10 || first.Deletions != 5 {
		t.Errorf("Files[0] adds/deletes = %d/%d, want 10/5", first.Additions, first.Deletions)
	}
}

func TestClient_FetchPR_FollowsPagination(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{
				"number": 1, "title": "x", "html_url": "u", "state": "open", "draft": false,
				"user": {"login": "u"},
				"base": {"ref": "main"}, "head": {"ref": "feat"},
				"additions": 2, "deletions": 0, "changed_files": 2,
				"labels": [], "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
			}`)
		case "/repos/o/r/pulls/1/files":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("page") == "2" {
				fmt.Fprintln(w, `[{"filename":"b.go","status":"added","additions":1,"deletions":0}]`)
				return
			}
			w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls/1/files?page=2>; rel="next"`, srv.URL))
			fmt.Fprintln(w, `[{"filename":"a.go","status":"added","additions":1,"deletions":0}]`)
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	pr, err := c.FetchPR(context.Background(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("FetchPR: %v", err)
	}

	if len(pr.Files) != 2 {
		t.Fatalf("len(Files) = %d, want 2", len(pr.Files))
	}
	if pr.Files[0].Path != "a.go" || pr.Files[1].Path != "b.go" {
		t.Errorf("Files = [%q, %q], want [a.go, b.go]", pr.Files[0].Path, pr.Files[1].Path)
	}
}

func TestClient_FetchPR_RejectsOffHostNextLink(t *testing.T) {
	t.Parallel()

	// Stand up a second "attacker" server. The legit server's Link header
	// will point here. The security property: the client must not contact
	// this server when it appears in a response from a different origin.
	var attackerHits atomic.Int32
	attackerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attackerHits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, `[]`)
	}))
	t.Cleanup(attackerSrv.Close)

	legitSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/o/r/pulls/1":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, `{
				"number": 1, "title": "x", "html_url": "u", "state": "open", "draft": false,
				"user": {"login": "u"},
				"base": {"ref": "main"}, "head": {"ref": "feat"},
				"additions": 0, "deletions": 0, "changed_files": 0,
				"labels": [], "created_at": "2026-01-01T00:00:00Z", "updated_at": "2026-01-01T00:00:00Z"
			}`)
		case "/repos/o/r/pulls/1/files":
			w.Header().Set("Content-Type", "application/json")
			// Hostile Link header: points at a different host (the attacker server).
			w.Header().Set("Link", fmt.Sprintf(`<%s/files?page=2>; rel="next"`, attackerSrv.URL))
			fmt.Fprintln(w, `[]`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(legitSrv.Close)

	c := github.NewClient(legitSrv.Client(), legitSrv.URL)
	_, err := c.FetchPR(context.Background(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err == nil {
		t.Fatal("expected error when next-link points off-origin, got nil")
	}
	if hits := attackerHits.Load(); hits > 0 {
		t.Errorf("client followed off-origin link to attacker server (%d hits); a token would have traveled there", hits)
	}
}

func TestClient_FetchPR_404Error(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintln(w, `{"message": "Not Found"}`)
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	_, err := c.FetchPR(context.Background(), analyzer.PRRef{Owner: "x", Repo: "y", Number: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
