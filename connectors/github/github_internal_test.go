package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

// TestClient_BodySizeLimitEnforcement covers both error paths a hostile
// or compromised server can drive the body-size limit into:
//
//  1. body that's malformed when truncated → decode error
//  2. valid JSON followed by trailing slack that pushes us past the limit
//     → the dedicated "exceeds %d-byte limit" error (the limited.N <= 0
//     branch in getJSON)
//
// Both must propagate as an error. Without the second sub-case the
// "exceeds" branch in production has zero coverage.
func TestClient_BodySizeLimitEnforcement(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		body            string
		maxBytes        int64
		wantErrContains string
	}{
		{
			name:            "body malformed when truncated → decode error",
			body:            `{"number":1,"title":"` + strings.Repeat("x", 500) + `"}`,
			maxBytes:        50,
			wantErrContains: "decode",
		},
		{
			name:            "valid JSON plus trailing slack → exceeds-limit error",
			body:            `{"number":1}` + strings.Repeat(" ", 200),
			maxBytes:        20,
			wantErrContains: "exceeds",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)

			c := NewClient(srv.Client(), srv.URL)
			c.maxResponseBytes = tc.maxBytes

			_, err := c.FetchPR(context.Background(), analyzer.PRRef{Owner: "o", Repo: "r", Number: 1})
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantErrContains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErrContains)
			}
		})
	}
}
