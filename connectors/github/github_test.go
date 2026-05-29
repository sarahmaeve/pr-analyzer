package github_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/connectors/github"
)

// TestClient_FetchPR_TrapdoorFixture loads the fabricated Trapdoor-
// shape fixture (agentforge/copilot-toolkit#47) through the connector
// and asserts every parsed field that the renderer downstream depends
// on. This is the connector-level mirror of the cmd/pr-analyzer smoke
// test — it gives a fast signal if a future fixture edit silently
// breaks the parse path.
func TestClient_FetchPR_TrapdoorFixture(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/agentforge/copilot-toolkit/pulls/47", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "testdata/pr_trapdoor.json")
	})
	mux.HandleFunc("/repos/agentforge/copilot-toolkit/pulls/47/files", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "testdata/pr_trapdoor_files.json")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	pr, err := c.FetchPR(t.Context(), analyzer.PRRef{
		Owner: "agentforge", Repo: "copilot-toolkit", Number: 47,
	})
	if err != nil {
		t.Fatalf("FetchPR error: %v", err)
	}

	if pr.Author != "secaudit-helper2026" {
		t.Errorf("Author = %q, want secaudit-helper2026", pr.Author)
	}
	if pr.Additions != 78 || pr.Deletions != 0 {
		t.Errorf("adds/deletes = %d/%d, want 78/0", pr.Additions, pr.Deletions)
	}
	if pr.ChangedFiles != 2 || len(pr.Files) != 2 {
		t.Fatalf("changed_files=%d / len(Files)=%d, want 2/2", pr.ChangedFiles, len(pr.Files))
	}

	wantPaths := []string{".cursorrules", "CLAUDE.md"}
	for i, w := range wantPaths {
		if pr.Files[i].Path != w {
			t.Errorf("Files[%d].Path = %q, want %q", i, pr.Files[i].Path, w)
		}
		if pr.Files[i].Status != "added" {
			t.Errorf("Files[%d].Status = %q, want added", i, pr.Files[i].Status)
		}
	}

	if pr.AuthorAssociation != "FIRST_TIME_CONTRIBUTOR" {
		t.Errorf("AuthorAssociation = %q, want FIRST_TIME_CONTRIBUTOR", pr.AuthorAssociation)
	}
}

// TestFixture_TrapdoorPatchesMatchHunkHeaders enforces an internal-
// consistency contract on the trapdoor fixture: every patch entry's
// unified-diff hunk header (@@ -…,… +…,N @@) must declare a count N
// that equals the number of '+'-prefixed lines actually present in
// the patch body. Real GitHub responses are consistent in this way;
// the fixture must be too, or we end up testing pr-analyzer against
// shapes it would never see in production traffic.
func TestFixture_TrapdoorPatchesMatchHunkHeaders(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile("testdata/pr_trapdoor_files.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var files []struct {
		Filename string `json:"filename"`
		Patch    string `json:"patch"`
	}
	if err := json.Unmarshal(data, &files); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("fixture has zero file entries")
	}

	// Matches the "added" hunk header shape: @@ -0,0 +1,N @@.
	hunkRe := regexp.MustCompile(`^@@ -\d+,\d+ \+\d+,(\d+) @@`)
	for _, f := range files {
		m := hunkRe.FindStringSubmatch(f.Patch)
		if m == nil {
			t.Errorf("%s: patch does not start with a recognizable hunk header", f.Filename)
			continue
		}
		claimed, err := strconv.Atoi(m[1])
		if err != nil {
			t.Errorf("%s: hunk header count is not an integer: %v", f.Filename, err)
			continue
		}

		// Count '+'-prefixed lines in the body. A real diff's file-marker
		// line starts with '+++', which the connector strips; the fixture's
		// patch field carries hunk content only (no '+++' marker), so any
		// '+' at line-start counts as an added line.
		actual := 0
		for line := range strings.SplitSeq(f.Patch, "\n") {
			if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
				actual++
			}
		}
		if actual != claimed {
			t.Errorf("%s: hunk header claims %d added lines but body contains %d '+' lines",
				f.Filename, claimed, actual)
		}
	}
}

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
	pr, err := c.FetchPR(t.Context(), analyzer.PRRef{
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
	if pr.AuthorAssociation != "OWNER" {
		t.Errorf("AuthorAssociation = %q, want OWNER", pr.AuthorAssociation)
	}
}

// TestClient_FetchPR_SurfacesCommitSHAs pins that the connector carries
// the base/head commit SHAs from the PR-detail response. They are needed
// by downstream deep analysis (signatory checks out / reads blobs at the
// exact head commit) and were previously discarded by refPayload.
func TestClient_FetchPR_SurfacesCommitSHAs(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r/pulls/1", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"number":1,"title":"x","html_url":"u","state":"open","draft":false,
			"user":{"login":"u","type":"User"},
			"base":{"ref":"main","sha":"baaaaaa0000000000000000000000000000000a"},
			"head":{"ref":"feat","sha":"heeeeee1111111111111111111111111111111b"},
			"additions":1,"deletions":0,"changed_files":1,"labels":[],
			"author_association":"CONTRIBUTOR",
			"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`)
	})
	mux.HandleFunc("/repos/o/r/pulls/1/files", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `[]`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	pr, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err != nil {
		t.Fatalf("FetchPR error: %v", err)
	}
	if pr.HeadSHA != "heeeeee1111111111111111111111111111111b" {
		t.Errorf("HeadSHA = %q, want head sha", pr.HeadSHA)
	}
	if pr.BaseSHA != "baaaaaa0000000000000000000000000000000a" {
		t.Errorf("BaseSHA = %q, want base sha", pr.BaseSHA)
	}
	if pr.AuthorType != "User" {
		t.Errorf("AuthorType = %q, want User", pr.AuthorType)
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
	pr, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
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
	_, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err == nil {
		t.Fatal("expected error when next-link points off-origin, got nil")
	}
	if hits := attackerHits.Load(); hits > 0 {
		t.Errorf("client followed off-origin link to attacker server (%d hits); a token would have traveled there", hits)
	}
}

func TestClient_FetchPR_MalformedJSON(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not valid JSON at all`))
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	_, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err == nil {
		t.Fatal("expected decode error, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Errorf("error %v does not mention decode failure", err)
	}
}

func TestClient_FetchPR_ContextCancellation(t *testing.T) {
	t.Parallel()

	// Server blocks until the client cancels — proves the client honors
	// ctx mid-flight rather than waiting for the server to respond.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already-cancelled context; the first request must fail immediately

	_, err := c.FetchPR(ctx, analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error %v does not wrap context.Canceled", err)
	}
}

func TestClient_FetchPR_SendsRequiredHeaders(t *testing.T) {
	t.Parallel()

	type seenReq struct {
		path    string
		accept  string
		version string
	}
	var (
		mu       sync.Mutex
		captured []seenReq
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		captured = append(captured, seenReq{
			path:    r.URL.Path,
			accept:  r.Header.Get("Accept"),
			version: r.Header.Get("X-GitHub-Api-Version"),
		})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/files") {
			fmt.Fprintln(w, `[]`)
			return
		}
		fmt.Fprintln(w, `{"number":1,"title":"x","html_url":"u","state":"open","draft":false,"user":{"login":"u"},"base":{"ref":"main"},"head":{"ref":"feat"},"additions":0,"deletions":0,"changed_files":0,"labels":[],"created_at":"2026-01-01T00:00:00Z","updated_at":"2026-01-01T00:00:00Z"}`)
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	if _, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1}); err != nil {
		t.Fatalf("FetchPR: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(captured) < 2 {
		t.Fatalf("captured %d requests, want at least 2 (PR detail + files)", len(captured))
	}
	// Every outbound request must carry both headers — checking only the
	// last would let a regression that drops them on, say, paginated
	// requests slip through.
	for _, req := range captured {
		if req.accept != "application/vnd.github+json" {
			t.Errorf("%s: Accept = %q, want %q", req.path, req.accept, "application/vnd.github+json")
		}
		if req.version != "2022-11-28" {
			t.Errorf("%s: X-GitHub-Api-Version = %q, want %q", req.path, req.version, "2022-11-28")
		}
	}
}

// TestClient_ListOpenPRs_SignatoryFixture pins the parse path for the
// listing endpoint introduced in slice 5. The fixture is the body of
// a real-shape /pulls?state=open response trimmed to the fields the
// connector reads; the assertion proves Number / Owner / Repo land on
// the returned PRRefs in list order.
func TestClient_ListOpenPRs_SignatoryFixture(t *testing.T) {
	t.Parallel()

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/sarahmaeve/signatory/pulls", func(w http.ResponseWriter, r *http.Request) {
		// The connector must send state=open; otherwise the upstream
		// returns closed PRs too and the report is wrong. Pin it.
		if got := r.URL.Query().Get("state"); got != "open" {
			t.Errorf("?state = %q, want open", got)
		}
		w.Header().Set("Content-Type", "application/json")
		http.ServeFile(w, r, "testdata/signatory_pulls_open.json")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	refs, err := c.ListOpenPRs(t.Context(), "sarahmaeve", "signatory")
	if err != nil {
		t.Fatalf("ListOpenPRs: %v", err)
	}

	want := []analyzer.PRRef{
		{Owner: "sarahmaeve", Repo: "signatory", Number: 144},
		{Owner: "sarahmaeve", Repo: "signatory", Number: 142},
		{Owner: "sarahmaeve", Repo: "signatory", Number: 141},
	}
	if len(refs) != len(want) {
		t.Fatalf("len(refs) = %d, want %d", len(refs), len(want))
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("refs[%d] = %+v, want %+v", i, refs[i], want[i])
		}
	}
}

func TestClient_ListOpenPRs_FollowsPagination(t *testing.T) {
	t.Parallel()

	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/o/r/pulls" {
			t.Errorf("unexpected path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("page") == "2" {
			fmt.Fprintln(w, `[{"number": 12, "html_url": "https://github.com/o/r/pull/12"}]`)
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/repos/o/r/pulls?state=open&page=2>; rel="next"`, srv.URL))
		fmt.Fprintln(w, `[{"number": 11, "html_url": "https://github.com/o/r/pull/11"}]`)
	}))
	t.Cleanup(srv.Close)

	c := github.NewClient(srv.Client(), srv.URL)
	refs, err := c.ListOpenPRs(t.Context(), "o", "r")
	if err != nil {
		t.Fatalf("ListOpenPRs: %v", err)
	}
	if len(refs) != 2 {
		t.Fatalf("len(refs) = %d, want 2", len(refs))
	}
	if refs[0].Number != 11 || refs[1].Number != 12 {
		t.Errorf("refs = [%d, %d], want [11, 12]", refs[0].Number, refs[1].Number)
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
	_, err := c.FetchPR(t.Context(), analyzer.PRRef{Owner: "x", Repo: "y", Number: 1})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// The error should identify the HTTP status — a regression that swallowed
	// non-200s and returned an empty PR would still produce "some error" but
	// would hide the cause.
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error %v does not mention status 404", err)
	}
}
