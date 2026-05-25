package json_test

import (
	stdjson "encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
	"github.com/sarahmaeve/pr-analyzer/render/json"
)

// TestAnalysis_jsonTagsAreSnakeCase pins the wire-format contract.
// Every key consumers of analyses.json depend on is asserted here, so
// a regression that adds a field without a tag (which would emit a
// PascalCase key) or renames an existing tag fails loud — before any
// downstream tool starts seeing the wrong shape.
//
// The list is authoritative: when a new field lands on Analysis or
// one of its child structs, it must appear here with its expected
// snake_case spelling.
func TestAnalysis_jsonTagsAreSnakeCase(t *testing.T) {
	t.Parallel()

	a := analyzer.Analysis{
		PR: analyzer.PR{
			Ref:               analyzer.PRRef{Owner: "o", Repo: "r", Number: 1},
			Title:             "t",
			Author:            "u",
			URL:               "https://example/1",
			State:             "open",
			Draft:             false,
			BaseRef:           "main",
			HeadRef:           "feat",
			Additions:         10,
			Deletions:         2,
			ChangedFiles:      3,
			Labels:            []string{"bug"},
			AuthorAssociation: "FIRST_TIME_CONTRIBUTOR",
			CreatedAt:         time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC),
			UpdatedAt:         time.Date(2026, 5, 25, 11, 0, 0, 0, time.UTC),
			Files: []analyzer.PRFile{
				{Path: "a.go", Status: "added", Additions: 10, Deletions: 0},
			},
		},
		CodeShape: codeshape.Signals{
			LOC:                     codeshape.LOC{Additions: 10, Deletions: 2, Total: 12},
			TestsTouched:            true,
			ManifestsTouched:        []string{"go.mod"},
			Languages:               []string{"Go"},
			RiskyPathsTouched:       []string{"billing/x.go"},
			AgentConfigPathsTouched: []string{"CLAUDE.md"},
			ExceedsMaxLOC:           true,
			MaxLOCThreshold:         5,
			LanguagesByPosture: codeshape.LanguagesByPosture{
				Preferred: []string{"Go"},
				Allowed:   []string{"TypeScript"},
				Anomalous: []string{"Rust"},
			},
		},
		EngineerProfile: engineerprofile.Signals{
			AuthorAssociation: "FIRST_TIME_CONTRIBUTOR",
		},
	}

	body, err := stdjson.Marshal(a)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(body)

	// Each entry is one (struct field path → wire key) contract.
	// Lower-case prefix on the key proves a json tag exists; without
	// a tag, encoding/json emits the field name verbatim (PascalCase).
	wantKeys := []string{
		// analyzer.Analysis
		`"pr":`,
		`"code_shape":`,
		`"engineer_profile":`,
		// analyzer.PR
		`"ref":`,
		`"title":`,
		`"author":`,
		`"url":`,
		`"state":`,
		`"draft":`,
		`"base_ref":`,
		`"head_ref":`,
		`"additions":`,
		`"deletions":`,
		`"changed_files":`,
		`"labels":`,
		`"files":`,
		`"created_at":`,
		`"updated_at":`,
		`"author_association":`,
		// analyzer.PRRef
		`"owner":`,
		`"repo":`,
		`"number":`,
		// analyzer.PRFile
		`"path":`,
		`"status":`,
		// codeshape.Signals
		`"loc":`,
		`"tests_touched":`,
		`"manifests_touched":`,
		`"languages":`,
		`"risky_paths_touched":`,
		`"agent_config_paths_touched":`,
		`"exceeds_max_loc":`,
		`"max_loc_threshold":`,
		`"languages_by_posture":`,
		// codeshape.LanguagesByPosture
		`"preferred":`,
		`"allowed":`,
		`"anomalous":`,
		// codeshape.LOC — sub-fields land on the inner object
		// (Additions / Deletions / Total). The top-level analyzer.PR
		// also has "additions" / "deletions", so we cannot disambiguate
		// by substring here; add the "total" key as the unique LOC marker.
		`"total":`,
	}

	for _, k := range wantKeys {
		if !strings.Contains(got, k) {
			t.Errorf("marshal output missing key %s\nbody: %s", k, got)
		}
	}

	// Round-trip: the marshalled bytes must decode back into the
	// same structural shape. Cheap insurance that the tags aren't
	// only-write — encoding/json reads tags too.
	var back analyzer.Analysis
	if err := stdjson.Unmarshal(body, &back); err != nil {
		t.Fatalf("round-trip unmarshal: %v", err)
	}
	if back.PR.Ref.Number != 1 || back.PR.AuthorAssociation != "FIRST_TIME_CONTRIBUTOR" {
		t.Errorf("round-trip lost fields: %+v", back.PR)
	}
	if !slices.Equal(back.CodeShape.Languages, []string{"Go"}) {
		t.Errorf("round-trip lost CodeShape.Languages: %v", back.CodeShape.Languages)
	}
	if back.EngineerProfile.AuthorAssociation != "FIRST_TIME_CONTRIBUTOR" {
		t.Errorf("round-trip lost EngineerProfile.AuthorAssociation: %q", back.EngineerProfile.AuthorAssociation)
	}
}

// TestRender_envelope decodes the bytes Render produces and asserts
// the envelope's fields. Pins schema_version (load-bearing for future
// downstream tools), the injected generated_at (no hidden time.Now
// inside Render), the repo block, and that analyses round-trip.
func TestRender_envelope(t *testing.T) {
	t.Parallel()

	repo := analyzer.PRRef{Owner: "Kong", Repo: "kong"} // Number zero in list mode
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	analyses := []analyzer.Analysis{
		{PR: analyzer.PR{Ref: analyzer.PRRef{Owner: "Kong", Repo: "kong", Number: 14838}, Author: "bungle"}},
		{PR: analyzer.PR{Ref: analyzer.PRRef{Owner: "Kong", Repo: "kong", Number: 14841}, Author: "other"}},
	}

	body, err := json.Render(analyses, repo, now)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	var env struct {
		SchemaVersion int                 `json:"schema_version"`
		GeneratedAt   time.Time           `json:"generated_at"`
		Repo          analyzer.PRRef      `json:"repo"`
		Analyses      []analyzer.Analysis `json:"analyses"`
	}
	if err := stdjson.Unmarshal(body, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v\nbody: %s", err, body)
	}

	if env.SchemaVersion != 1 {
		t.Errorf("schema_version = %d, want 1", env.SchemaVersion)
	}
	if !env.GeneratedAt.Equal(now) {
		t.Errorf("generated_at = %v, want %v", env.GeneratedAt, now)
	}
	if env.Repo.Owner != "Kong" || env.Repo.Repo != "kong" {
		t.Errorf("repo = %+v, want Kong/kong", env.Repo)
	}
	if len(env.Analyses) != 2 {
		t.Fatalf("len(analyses) = %d, want 2", len(env.Analyses))
	}
	if env.Analyses[0].PR.Ref.Number != 14838 || env.Analyses[1].PR.Ref.Number != 14841 {
		t.Errorf("analyses PR numbers = [%d, %d], want [14838, 14841]",
			env.Analyses[0].PR.Ref.Number, env.Analyses[1].PR.Ref.Number)
	}
}

// TestRender_emptyAnalyses pins the zero-PR case: Render must still
// emit a valid envelope, not nil bytes. A repo with no open PRs is a
// legitimate state — surfacing "scanning 0 PRs" with an envelope that
// downstream tooling can still parse is friendlier than emitting
// nothing.
func TestRender_emptyAnalyses(t *testing.T) {
	t.Parallel()

	body, err := json.Render(nil, analyzer.PRRef{Owner: "o", Repo: "r"}, time.Now())
	if err != nil {
		t.Fatalf("Render with nil analyses: %v", err)
	}
	if !strings.Contains(string(body), `"schema_version"`) {
		t.Errorf("envelope missing schema_version on empty input: %s", body)
	}
	if !strings.Contains(string(body), `"analyses"`) {
		t.Errorf("envelope missing analyses key on empty input: %s", body)
	}
}

// TestRender_deterministic proves Render takes `now` as a parameter
// and uses no hidden time.Now() call internally — back-to-back calls
// with the same inputs must produce identical bytes. Without this,
// snapshot tests on the JSON output would flake.
func TestRender_deterministic(t *testing.T) {
	t.Parallel()

	repo := analyzer.PRRef{Owner: "o", Repo: "r"}
	now := time.Date(2026, 5, 25, 10, 0, 0, 0, time.UTC)
	a := []analyzer.Analysis{{PR: analyzer.PR{Ref: analyzer.PRRef{Owner: "o", Repo: "r", Number: 1}}}}

	b1, err := json.Render(a, repo, now)
	if err != nil {
		t.Fatalf("Render #1: %v", err)
	}
	b2, err := json.Render(a, repo, now)
	if err != nil {
		t.Fatalf("Render #2: %v", err)
	}
	if string(b1) != string(b2) {
		t.Errorf("Render is not deterministic:\nb1: %s\nb2: %s", b1, b2)
	}
}
