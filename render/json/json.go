// Package json renders an analyzer.Analysis slice as a JSON document
// suitable for downstream tooling (humans with jq, dashboards,
// LLM-driven review bots, the future HTML renderer that embeds this
// document inline). The output is the canonical machine-readable
// artifact pr-analyzer produces in list mode.
//
// Package name collides with stdlib encoding/json; callers that need
// both alias one of them at import time, e.g.
//
//	import (
//	    stdjson "encoding/json"
//	    rjson "github.com/sarahmaeve/pr-analyzer/render/json"
//	)
package json

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

// SchemaVersion is the integer version of the Envelope shape. Bumped
// when an existing key changes meaning or is removed; additive
// changes (new optional fields) leave it unchanged. Consumers
// fingerprint on it.
const SchemaVersion = 1

// Envelope wraps a list-mode scan result with the metadata downstream
// tools need to interpret it: the schema version they should parse
// against, the time the scan completed, the target repo (Number is
// zero — repo-scoped scans have no PR-level number), and the
// analyses themselves in the order ListOpenPRs returned them.
type Envelope struct {
	SchemaVersion int                 `json:"schema_version"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Repo          analyzer.PRRef      `json:"repo"`
	Analyses      []analyzer.Analysis `json:"analyses"`
}

// Render serializes analyses into the machine-readable envelope.
// `now` is taken as a parameter so callers (and tests) control the
// generated_at timestamp; Render itself reads no clock and produces
// deterministic output for identical inputs.
//
// Output is indented with two spaces for human readability — the
// artifact is meant to be inspected with jq / grep / a text editor
// as readily as it is consumed programmatically. Embedders that need
// compact output should re-encode the Envelope.
func Render(analyses []analyzer.Analysis, repo analyzer.PRRef, now time.Time) ([]byte, error) {
	env := Envelope{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   now,
		Repo:          repo,
		Analyses:      analyses,
	}
	body, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal analyses envelope: %w", err)
	}
	return body, nil
}
