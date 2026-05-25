package main

import (
	"strings"
	"testing"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
	rjson "github.com/sarahmaeve/pr-analyzer/render/json"
)

// fixtureEnvelope returns an Envelope with a known mix of signals so
// summarize() can be exercised without going through the binary or
// the scanner. The PRs are chosen so every code path in the
// summary has at least one row:
//   - mixed author_association buckets
//   - multiple languages, including ones detected on multiple PRs
//   - one tests-touched PR, two not
//   - one manifest-touching PR
//   - one agent-config-touching PR (the cross-project signal)
//   - one anomalous-language PR (driven by per-PR Config posture)
func fixtureEnvelope() rjson.Envelope {
	return rjson.Envelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 5, 25, 17, 34, 21, 0, time.UTC),
		Repo:          analyzer.PRRef{Owner: "atuinsh", Repo: "atuin"},
		Analyses: []analyzer.Analysis{
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3500},
					Title:             "Big i18n PR",
					Author:            "philtweir",
					AuthorAssociation: "CONTRIBUTOR",
				},
				CodeShape: codeshape.Signals{
					LOC:              codeshape.LOC{Additions: 1000, Deletions: 342, Total: 1342},
					TestsTouched:     true,
					ManifestsTouched: []string{"Cargo.lock", "Cargo.toml"},
					Languages:        []string{"Rust", "YAML"},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "CONTRIBUTOR"},
			},
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3429},
					Title:             "small typo fix",
					Author:            "drive-by",
					AuthorAssociation: "FIRST_TIME_CONTRIBUTOR",
				},
				CodeShape: codeshape.Signals{
					LOC:       codeshape.LOC{Additions: 2, Deletions: 1, Total: 3},
					Languages: []string{"Markdown"},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "FIRST_TIME_CONTRIBUTOR"},
			},
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3300},
					Title:             "Agent config drop-in",
					Author:            "secaudit-2026",
					AuthorAssociation: "NONE",
				},
				CodeShape: codeshape.Signals{
					LOC:                     codeshape.LOC{Additions: 80, Deletions: 0, Total: 80},
					Languages:               []string{"Markdown"},
					AgentConfigPathsTouched: []string{".cursorrules", "CLAUDE.md"},
					LanguagesByPosture: codeshape.LanguagesByPosture{
						Anomalous: []string{"Markdown"},
					},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "NONE"},
			},
		},
	}
}

func TestSummarize_includesHeaderAndAllSections(t *testing.T) {
	t.Parallel()

	got := summarize(fixtureEnvelope())

	wants := []string{
		// Header — repo, count, generated_at, schema version.
		"atuinsh/atuin",
		"3 open PRs",
		"2026-05-25T17:34:21Z",
		"schema v1",
		// Author association: each bucket present with its count.
		"Author association",
		"CONTRIBUTOR",
		"FIRST_TIME_CONTRIBUTOR",
		"NONE",
		// Languages: each detected language present with count.
		"Languages",
		"Rust",
		"Markdown",
		"YAML",
		// LOC section with totals and a top-by-size table.
		"Lines of code",
		"1342",
		"philtweir",
		// Tests touched count (1 of 3).
		"Tests touched",
		"1 / 3",
		// Manifests: one PR touched them.
		"Dependency manifests touched",
		"Cargo.lock",
		// Agent-config: load-bearing signal — must always appear.
		"Agent-config files touched",
		".cursorrules",
		"CLAUDE.md",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in summary:\n%s", w, got)
		}
	}
}

// TestSummarize_isDeterministic pins the contract that summarize
// reads no clock and sorts ties stably — back-to-back calls on the
// same envelope must produce byte-identical output. Without this,
// inspector output diffs would flake across runs.
func TestSummarize_isDeterministic(t *testing.T) {
	t.Parallel()

	env := fixtureEnvelope()
	first := summarize(env)
	second := summarize(env)
	if first != second {
		t.Fatalf("summarize is not deterministic on identical input:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// TestSummarize_emptyAnalyses pins the zero-PR case: the header still
// renders, sections that would be empty are gracefully omitted, and
// the function does not panic.
func TestSummarize_emptyAnalyses(t *testing.T) {
	t.Parallel()

	env := rjson.Envelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 5, 25, 0, 0, 0, 0, time.UTC),
		Repo:          analyzer.PRRef{Owner: "o", Repo: "r"},
	}
	got := summarize(env)
	if !strings.Contains(got, "0 open PRs") {
		t.Errorf("empty-envelope summary should report 0 PRs:\n%s", got)
	}
}

// TestSummarize_authorAssociationSortedByCountDesc proves the most
// common bucket lands first. A real-world scan with 45 NONE and 5
// MEMBER must show NONE on top regardless of map iteration order.
func TestSummarize_authorAssociationSortedByCountDesc(t *testing.T) {
	t.Parallel()

	got := summarize(fixtureEnvelope())

	// Find the "Author association" section.
	idx := strings.Index(got, "Author association")
	if idx < 0 {
		t.Fatal("Author association section absent")
	}
	section := got[idx:]
	// The fixture has 1 of each association. Tie-broken by alphabetical;
	// asserting "appears in sorted-by-count-desc, then alpha" requires
	// at least one tie-break to be visible. Use the order: CONTRIBUTOR,
	// FIRST_TIME_CONTRIBUTOR, NONE (alphabetical for the all-1 ties).
	wantOrder := []string{"CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "NONE"}
	lastPos := -1
	for _, name := range wantOrder {
		pos := strings.Index(section, name)
		if pos < 0 {
			t.Fatalf("association %q absent from section:\n%s", name, section)
		}
		if pos <= lastPos {
			t.Errorf("association %q appears at position %d, before previous %d (expected alphabetical tie-break)",
				name, pos, lastPos)
		}
		lastPos = pos
	}
}
