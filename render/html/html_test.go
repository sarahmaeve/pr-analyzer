package html_test

import (
	stdjson "encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
	"github.com/sarahmaeve/pr-analyzer/render/html"
	rjson "github.com/sarahmaeve/pr-analyzer/render/json"
)

// fixtureEnvelope returns an envelope with the spread of signals each
// renderer code path needs to exercise:
//   - one PR with the trusted OWNER bucket (no pill)
//   - one PR with FIRST_TIME_CONTRIBUTOR (orange warning pill)
//   - one PR with agent-config + MANNEQUIN (red severity pills)
//   - one PR with anomalous languages and exceeds_max_loc
func fixtureEnvelope() rjson.Envelope {
	return rjson.Envelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Date(2026, 5, 25, 17, 34, 21, 0, time.UTC),
		Repo:          analyzer.PRRef{Owner: "atuinsh", Repo: "atuin"},
		Analyses: []analyzer.Analysis{
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3500},
					Title:             "Big refactor (linked title)",
					Author:            "ellie",
					URL:               "https://github.com/atuinsh/atuin/pull/3500",
					Additions:         500,
					Deletions:         100,
					ChangedFiles:      12,
					AuthorAssociation: "OWNER",
				},
				CodeShape: codeshape.Signals{
					LOC:          codeshape.LOC{Additions: 500, Deletions: 100, Total: 600},
					TestsTouched: true,
					Languages:    []string{"Rust", "YAML"},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "OWNER"},
			},
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3429},
					Title:             "Small typo fix",
					Author:            "drive-by",
					URL:               "https://github.com/atuinsh/atuin/pull/3429",
					Additions:         2,
					Deletions:         1,
					ChangedFiles:      1,
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
					Title:             "Suspicious: agent-config drop-in",
					Author:            "secaudit-2026",
					URL:               "https://github.com/atuinsh/atuin/pull/3300",
					Additions:         80,
					Deletions:         0,
					ChangedFiles:      2,
					AuthorAssociation: "MANNEQUIN",
				},
				CodeShape: codeshape.Signals{
					LOC:                     codeshape.LOC{Additions: 80, Deletions: 0, Total: 80},
					Languages:               []string{"Markdown"},
					AgentConfigPathsTouched: []string{".cursorrules", "CLAUDE.md"},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "MANNEQUIN"},
			},
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3100},
					Title:             "Bump some dependency",
					Author:            "dependabot[bot]",
					URL:               "https://github.com/atuinsh/atuin/pull/3100",
					Additions:         12,
					Deletions:         8,
					ChangedFiles:      2,
					AuthorAssociation: "CONTRIBUTOR",
				},
				CodeShape: codeshape.Signals{
					LOC:              codeshape.LOC{Additions: 12, Deletions: 8, Total: 20},
					Languages:        []string{"Rust"},
					ManifestsTouched: []string{"Cargo.lock", "Cargo.toml"},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "CONTRIBUTOR"},
			},
			{
				PR: analyzer.PR{
					Ref:               analyzer.PRRef{Owner: "atuinsh", Repo: "atuin", Number: 3200},
					Title:             "Adds Rust to a Go project",
					Author:            "polyglot",
					URL:               "https://github.com/atuinsh/atuin/pull/3200",
					Additions:         1500,
					Deletions:         50,
					ChangedFiles:      20,
					AuthorAssociation: "CONTRIBUTOR",
				},
				CodeShape: codeshape.Signals{
					LOC:               codeshape.LOC{Additions: 1500, Deletions: 50, Total: 1550},
					Languages:         []string{"Rust", "Go"},
					RiskyPathsTouched: []string{"billing/charge.go"},
					ExceedsMaxLOC:     true,
					MaxLOCThreshold:   1000,
					LanguagesByPosture: codeshape.LanguagesByPosture{
						Preferred: []string{"Go"},
						Anomalous: []string{"Rust"},
					},
				},
				EngineerProfile: engineerprofile.Signals{AuthorAssociation: "CONTRIBUTOR"},
			},
		},
	}
}

// TestRender_basicShape pins the page-level surface a user sees:
// repo header (linked to the GitHub repo), open-PR count, every PR
// in the input represented by its number + author + title (each
// hyperlinked), and the inline JSON data block.
func TestRender_basicShape(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	wants := []string{
		// HTML document basics.
		"<!doctype html>",
		"<html",
		`<meta charset="utf-8"`,
		// Header: repo linked to GitHub, count visible, generated_at + schema.
		`href="https://github.com/atuinsh/atuin"`,
		"atuinsh/atuin",
		"5 OPEN PRS",
		"2026-05-25T17:34:21Z",
		"schema v1",
		// Each PR shows up.
		"#3500", "ellie", "Big refactor",
		"#3429", "drive-by", "Small typo fix",
		"#3300", "secaudit-2026", "Suspicious: agent-config drop-in",
		"#3200", "polyglot", "Adds Rust to a Go project",
		"#3100", "dependabot[bot]", "Bump some dependency",
		// PR title links to the PR URL.
		`href="https://github.com/atuinsh/atuin/pull/3500"`,
		// Author handle links to the GitHub profile.
		`href="https://github.com/ellie"`,
		`href="https://github.com/drive-by"`,
		// Drill-down element exists for each PR.
		"<details>",
		// JSON data block.
		`<script type="application/json" id="pra-data">`,
		`"schema_version": 1`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q in HTML output", w)
		}
	}
}

// TestRender_stableMarkupContract pins the class and data-attribute
// surface users will skin against. A regression that renames any of
// these is a breaking change to consumer CSS — it fails here before
// it surprises anyone downstream.
func TestRender_stableMarkupContract(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	wants := []string{
		// Page-level classes.
		`class="pra-header"`,
		`class="pra-prs"`,
		`class="pra-pr"`,
		// Per-PR data attributes.
		`data-pra-pr-number="3500"`,
		`data-pra-pr-author="ellie"`,
		`data-pra-pr-author-association="OWNER"`,
		`data-pra-pr-loc-additions="500"`,
		`data-pra-pr-loc-deletions="100"`,
		`data-pra-pr-loc-total="600"`,
		`data-pra-pr-tests-touched="true"`,
		`data-pra-pr-agent-config-touched="false"`,
		// PR #3300 has agent-config touched — attribute flips.
		`data-pra-pr-agent-config-touched="true"`,
		// LOC bar segments — class names + percentage style.
		`class="pra-pr-bar"`,
		`class="pra-pr-bar-add"`,
		`class="pra-pr-bar-del"`,
		// Summary children.
		`class="pra-pr-number"`,
		`class="pra-pr-author"`,
		`class="pra-pr-title"`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing markup-contract token %q", w)
		}
	}
}

// TestRender_severityPills pins the alert-color routing.
//   - Severe (red): agent-config-touched, MANNEQUIN, exceeds-max-LOC.
//   - Warning (orange): FIRST_TIME_CONTRIBUTOR, risky paths, anomalous
//     languages, tests-not-touched.
//   - Positive (green): CONTRIBUTOR, tests-touched.
//
// PR #3500 is OWNER with no anomalies but has tests touched — it
// should have the positive tests pill but no author-association pill.
func TestRender_severityPills(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// PR #3500 (OWNER) must not get an author-association pill.
	pr3500 := isolatePR(t, out, 3500)
	if regexp.MustCompile(`pra-pill-\w+">OWNER`).MatchString(pr3500) {
		t.Errorf("PR #3500 (OWNER) should not have an author-association pill:\n%s", pr3500)
	}

	wants := []string{
		// Severe (red) pills.
		`pra-pill-danger`,
		"AGENT-CONFIG",
		"MANNEQUIN",
		"EXCEEDS MAX LOC",
		// Warning (orange) pills.
		`pra-pill-warning`,
		"FIRST_TIME_CONTRIBUTOR",
		"RISKY PATHS",
		"ANOMALOUS",
		"NOT TOUCHED",
		// Positive (green) pills — new in this iteration.
		`pra-pill-success`,
		"TOUCHED",
		"PREFERRED LANGUAGES",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing pill token %q in HTML output", w)
		}
	}

	// PR #3200 is CONTRIBUTOR — must wear the success (green) pill,
	// not warning. This is the regression test for the "CONTRIBUTOR is
	// positive signal" iteration.
	pr3200 := isolatePR(t, out, 3200)
	if !regexp.MustCompile(`pra-pill-success">CONTRIBUTOR`).MatchString(pr3200) {
		t.Errorf("PR #3200 (CONTRIBUTOR) should wear pra-pill-success, not warning. Section:\n%s", pr3200)
	}
}

// TestRender_locBarPerPRRatio pins the inside-the-fill split: the
// add and del segments still flex by per-PR adds-vs-deletes ratio so
// the visible color split inside each PR's bar tells the user
// "additions vs deletions" at a glance.
//
//	PR #3500: 500/100, total 600 → adds 83.3%, dels 16.7% of fill
//	PR #3300: 80/0,  total 80   → adds 100%, dels 0%
func TestRender_locBarPerPRRatio(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	pr3500Section := isolatePR(t, out, 3500)
	if !regexp.MustCompile(`pra-pr-bar-add[^>]*flex-basis:\s*83`).MatchString(pr3500Section) {
		t.Errorf("PR #3500 add segment should be ~83%% of fill; section:\n%s", pr3500Section)
	}
	if !regexp.MustCompile(`pra-pr-bar-del[^>]*flex-basis:\s*16`).MatchString(pr3500Section) {
		t.Errorf("PR #3500 del segment should be ~16%% of fill; section:\n%s", pr3500Section)
	}

	pr3300Section := isolatePR(t, out, 3300)
	if !regexp.MustCompile(`pra-pr-bar-add[^>]*flex-basis:\s*100`).MatchString(pr3300Section) {
		t.Errorf("PR #3300 add segment should be 100%% of fill; section:\n%s", pr3300Section)
	}
}

// TestRender_authorClass pins summary-row author-name coloring:
//   - login ending in "[bot]" → yellow (pra-pr-author-bot).
//   - association == CONTRIBUTOR (and not a bot) → green
//     (pra-pr-author-contributor).
//   - everything else → no modifier class; the base .pra-pr-author
//     color applies (reviewer inspects the drill-down for detail).
//
// PR #3100 in the fixture (dependabot[bot] with CONTRIBUTOR
// association) proves precedence: a bot that's also a long-running
// CONTRIBUTOR still gets the bot color, not the contributor color.
func TestRender_authorClass(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// PR #3100 is dependabot[bot] + CONTRIBUTOR — bot class must win.
	pr3100 := isolatePR(t, out, 3100)
	if !strings.Contains(pr3100, "pra-pr-author-bot") {
		t.Errorf("PR #3100 (dependabot[bot]) should carry pra-pr-author-bot; section:\n%s", pr3100)
	}
	if strings.Contains(pr3100, "pra-pr-author-contributor") {
		t.Errorf("PR #3100 is a bot — must not also carry pra-pr-author-contributor; section:\n%s", pr3100)
	}

	// PR #3200 is polyglot + CONTRIBUTOR — human contributor green.
	pr3200 := isolatePR(t, out, 3200)
	if !strings.Contains(pr3200, "pra-pr-author-contributor") {
		t.Errorf("PR #3200 (CONTRIBUTOR human) should carry pra-pr-author-contributor; section:\n%s", pr3200)
	}

	// PR #3500 is ellie + OWNER — no modifier class on the author link.
	pr3500 := isolatePR(t, out, 3500)
	if strings.Contains(pr3500, "pra-pr-author-bot") || strings.Contains(pr3500, "pra-pr-author-contributor") {
		t.Errorf("PR #3500 (OWNER, human) should have no author modifier class; section:\n%s", pr3500)
	}
}

// TestRender_filesBar pins the second bar that visualizes
// PR.ChangedFiles, log-scaled cross-PR like the LOC bar. The largest
// PR by files count must fill the lane; smaller PRs must visibly
// shorter. Includes a data-attr contract assertion for skinning.
func TestRender_filesBar(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// Stable markup contract for the new bar.
	for _, w := range []string{
		`class="pra-pr-files-bar"`,
		`class="pra-pr-files-bar-fill"`,
		`data-pra-pr-files-changed="20"`,
	} {
		if !strings.Contains(out, w) {
			t.Errorf("missing files-bar contract token %q", w)
		}
	}

	// PR #3200 has 20 files (the max in the fixture) — fill is 100%.
	pr3200 := isolatePR(t, out, 3200)
	if !regexp.MustCompile(`pra-pr-files-bar-fill[^>]*width:\s*100`).MatchString(pr3200) {
		t.Errorf("PR #3200 (max files = 20) files-bar should be 100%%; section:\n%s", pr3200)
	}

	// PR #3429 has 1 file — fill must be markedly shorter (< 50%) so
	// scanning the column reveals the amplitude difference.
	pr3429 := isolatePR(t, out, 3429)
	m := regexp.MustCompile(`pra-pr-files-bar-fill[^>]*width:\s*([\d.]+)`).FindStringSubmatch(pr3429)
	if m == nil {
		t.Fatalf("PR #3429 files-bar-fill width not parseable from section:\n%s", pr3429)
	}
	if w := parseFloat(t, m[1]); w >= 50 {
		t.Errorf("PR #3429 (1 file vs max 20) files-bar = %v%%, want < 50%%", w)
	}
}

// TestRender_locBarCrossPRScale pins the new cross-PR scaling: the
// fill's width is log-scaled against the report's max total LOC.
// PR #3200 has the largest total (1550) and must reach the full lane;
// PR #3429 (3 LOC, the smallest) must be markedly shorter than the
// largest. Without this, the bars would all be lane-width-normalized
// and a 5-LOC PR would visually equal a 5000-LOC PR.
func TestRender_locBarCrossPRScale(t *testing.T) {
	t.Parallel()

	out, err := html.Render(fixtureEnvelope())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// PR #3200 (max LOC = 1550) → fill width 100%.
	pr3200 := isolatePR(t, out, 3200)
	if !regexp.MustCompile(`pra-pr-bar-fill[^>]*width:\s*100`).MatchString(pr3200) {
		t.Errorf("PR #3200 (max LOC) bar-fill should be 100%% wide; section:\n%s", pr3200)
	}

	// PR #3429 (3 LOC) should fill far less than 100%. The log scale
	// gives log(4)/log(1551) ≈ 19%, but we just assert "less than 50"
	// so a future scale-curve tweak doesn't break the test for the
	// wrong reason — the regression we care about is "all bars at
	// 100%" or "ratio inverted", not exact log curve.
	pr3429 := isolatePR(t, out, 3429)
	m := regexp.MustCompile(`pra-pr-bar-fill[^>]*width:\s*([\d.]+)`).FindStringSubmatch(pr3429)
	if m == nil {
		t.Fatalf("PR #3429 bar-fill width not parseable from section:\n%s", pr3429)
	}
	if w := parseFloat(t, m[1]); w >= 50 {
		t.Errorf("PR #3429 (3 LOC vs max 1550) bar-fill width = %v%%, want < 50%% (cross-PR scaling not in effect)", w)
	}
}

func parseFloat(t *testing.T, s string) float64 {
	t.Helper()
	var f float64
	if _, err := fmt.Sscanf(s, "%f", &f); err != nil {
		t.Fatalf("parse %q as float: %v", s, err)
	}
	return f
}

// isolatePR returns the slice of `out` covering PR number `n` and
// stopping just before the next PR marker in the output. Used by
// tests that need to assert on one PR's section without picking up
// signal from a later PR. The next-PR boundary is derived from the
// output itself rather than a caller-supplied stop number, so the
// slice is always exactly one PR regardless of fixture order.
func isolatePR(t *testing.T, out string, n int) string {
	t.Helper()
	const marker = `data-pra-pr-number="`
	start := strings.Index(out, marker+strconv.Itoa(n)+`"`)
	if start < 0 {
		t.Fatalf("PR #%d marker not found", n)
	}
	searchFrom := start + len(marker)
	nextOffset := strings.Index(out[searchFrom:], marker)
	if nextOffset < 0 {
		return out[start:]
	}
	return out[start : searchFrom+nextOffset]
}

// TestRender_inlinedJSONRoundTrips proves the script-tag JSON block
// decodes back into the same envelope we rendered from. Without this,
// downstream tools reading the inlined data could see drift from the
// sibling analyses.json file.
func TestRender_inlinedJSONRoundTrips(t *testing.T) {
	t.Parallel()

	in := fixtureEnvelope()
	out, err := html.Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	body := extractDataBlock(t, out)
	var got rjson.Envelope
	if err := stdjson.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decode inline JSON: %v\nblock:\n%s", err, body)
	}
	if got.SchemaVersion != in.SchemaVersion {
		t.Errorf("schema_version: got %d, want %d", got.SchemaVersion, in.SchemaVersion)
	}
	if got.Repo != in.Repo {
		t.Errorf("repo: got %+v, want %+v", got.Repo, in.Repo)
	}
	if len(got.Analyses) != len(in.Analyses) {
		t.Errorf("analyses count: got %d, want %d", len(got.Analyses), len(in.Analyses))
	}
}

// TestRender_escapesScriptInjection proves the inlined-JSON path
// cannot be broken by user-controlled content. If a PR title
// contains "</script>" verbatim, that substring must NOT appear in
// the rendered output (it would close the data block early and let
// arbitrary HTML execute as JS in older browsers).
func TestRender_escapesScriptInjection(t *testing.T) {
	t.Parallel()

	env := fixtureEnvelope()
	env.Analyses[0].PR.Title = `Title with </script><img src=x onerror=alert(1)> injection`

	out, err := html.Render(env)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	// The literal "</script>" must not appear except as the legitimate
	// closer of the pra-data block itself. We assert by counting: one
	// </script> for the data block, and zero others.
	count := strings.Count(out, "</script>")
	if count != 1 {
		t.Errorf("expected exactly one </script> in output (the data block closer), got %d", count)
	}
	// The injected <img> payload must not survive as a real element —
	// the actual security property. After html/template's escape, "<img"
	// becomes "&lt;img" and is text content, not a parseable tag. A
	// regression that emits the substring as a real element fails here.
	if regexp.MustCompile(`<img\s+src`).MatchString(out) {
		t.Errorf("output contains executable <img> tag — XSS regression:\n%s", out)
	}
}

// extractDataBlock returns the JSON body of <script id="pra-data">.
// Test helper, not part of the public API.
func extractDataBlock(t *testing.T, out string) string {
	t.Helper()
	const openTag = `<script type="application/json" id="pra-data">`
	const closeTag = `</script>`
	_, after, ok := strings.Cut(out, openTag)
	if !ok {
		t.Fatal("data block open tag not found")
	}
	before, _, ok := strings.Cut(after, closeTag)
	if !ok {
		t.Fatal("data block close tag not found")
	}
	return strings.TrimSpace(before)
}

// TestRender_deterministic proves two renders of the same envelope
// produce byte-identical output. Render must read no clock and rely
// on no map-iteration order.
func TestRender_deterministic(t *testing.T) {
	t.Parallel()

	env := fixtureEnvelope()
	a, err := html.Render(env)
	if err != nil {
		t.Fatalf("first render: %v", err)
	}
	b, err := html.Render(env)
	if err != nil {
		t.Fatalf("second render: %v", err)
	}
	if a != b {
		t.Fatal("Render is not deterministic on identical input")
	}
}

// TestRender_emptyAnalyses proves a zero-PR scan still produces a
// valid page with the header and data block.
func TestRender_emptyAnalyses(t *testing.T) {
	t.Parallel()

	env := rjson.Envelope{
		SchemaVersion: 1,
		GeneratedAt:   time.Now(),
		Repo:          analyzer.PRRef{Owner: "o", Repo: "r"},
	}
	out, err := html.Render(env)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(out, `<script type="application/json" id="pra-data">`) {
		t.Error("empty-envelope render missing data block")
	}
	if !strings.Contains(out, "o/r") {
		t.Error("empty-envelope render missing repo header")
	}
}
