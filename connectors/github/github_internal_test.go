package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

// TestClient_RejectsOversizedResponse guards against a hostile or
// compromised server that returns an unbounded body, which would otherwise
// be slurped into memory by json.NewDecoder. The Client's per-request size
// limit is enforced via io.LimitedReader.
func TestClient_RejectsOversizedResponse(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := `{"number":1,"title":"` + strings.Repeat("x", 500) + `"}`
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	c := NewClient(srv.Client(), srv.URL)
	c.maxResponseBytes = 50 // way under the 500-byte response

	_, err := c.FetchPR(context.Background(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
	if err == nil {
		t.Fatal("expected error for oversized response, got nil")
	}
}
