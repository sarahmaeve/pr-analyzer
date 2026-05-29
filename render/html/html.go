// Package html renders an analyses envelope as a single self-contained
// HTML document — the human-facing artifact pr-analyzer emits in list
// mode alongside analyses.json. Styling follows the cinematic
// sci-fi screen graphics design language documented in
// design/PROTO5.md (and the source file the language was lifted
// from); the markup contract is the stable surface users skin
// against by overriding CSS custom properties on :root or by adding
// rules scoped to the documented pra-* class namespace.
//
// Package name collides with stdlib html/template; callers alias one
// of them at import time.
package html

import (
	"bytes"
	_ "embed"
	stdjson "encoding/json"
	"fmt"
	"html/template"
	"math"
	"strings"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	rjson "github.com/sarahmaeve/pr-analyzer/render/json"
)

//go:embed template.html.tmpl
var pageTemplate string

//go:embed style.css
var pageCSS string

//go:embed loader.js
var loaderJS string

// page is the data the template iterates over. Renderer-only fields
// (Count, GeneratedAtFormatted, CSS, Views) sit alongside the
// embedded Envelope so the template can reach the source repo /
// schema fields without indirection. The inline-JSON data block is
// appended outside the template — html/template's JS-context auto-
// escaping mangles a `<script type="application/json">` body, so we
// emit it as raw bytes after the safe-HTML render completes.
type page struct {
	rjson.Envelope
	Count                int
	GeneratedAtFormatted string
	CSS                  template.CSS
	Views                []analysisView
}

// analysisView wraps an analyzer.Analysis with precomputed
// presentation fields the template would otherwise have to derive
// inline. Keeping them in Go (not template funcs) makes the
// template easier to read and the logic easier to test.
type analysisView struct {
	analyzer.Analysis
	BarFillStyle           template.HTMLAttr // width % of the LOC lane, log-scaled against report max
	BarAddStyle            template.HTMLAttr // flex-basis % within the LOC fill (per-PR adds ratio)
	BarDelStyle            template.HTMLAttr // flex-basis % within the LOC fill (per-PR deletes ratio)
	FilesBarFillStyle      template.HTMLAttr // width % of the files lane, log-scaled against report max
	AgentConfigTouched     bool
	AssociationInteresting bool
	AssociationPillClass   string
	AuthorClass            string // modifier class on the summary-row author link; empty for "no special highlight"
}

// Pill is a status chip an enricher places in a drill-down, rendered in
// the report's pra-pill vocabulary. Tier selects the color treatment and
// should be one of "danger", "warning", "success", or "info"; the loader
// degrades any other value to "info" so an unknown tier never emits a
// broken class.
type Pill struct {
	Text string `json:"text"`
	Tier string `json:"tier"`
}

// Row is a labelled detail line in a drill-down — a dt/dd pair rendered
// in the same pra-pr-signals grid the mechanistic signals use.
type Row struct {
	Term   string `json:"term"`
	Detail string `json:"detail"`
}

// Section is one extra block an enricher adds to a PR's drill-down. It
// renders below the mechanistic signals, inside a pra-pr-deep section,
// reusing pra-pill / pra-pr-signals — the enricher supplies structured
// data, never HTML, so the look matches the rest of the report
// automatically (including any future restyle).
type Section struct {
	Title string `json:"title"`
	Pills []Pill `json:"pills,omitempty"`
	Rows  []Row  `json:"rows,omitempty"`
}

// Enrichment maps a PR number to the extra drill-down sections for that
// PR. This is the signatory-agnostic seam: pr-analyzer's report knows how
// to render the sections in its own style but nothing about what produced
// them (a deep content scan, a burn ledger, ...). The enrichment is
// delivered out-of-band — see SidecarJS — so index.html is byte-identical
// whether or not anything was scanned, and the deep layer is purely
// additive.
type Enrichment map[int][]Section

const (
	// SidecarFilename is the file an enricher writes NEXT TO index.html.
	// The report's in-page loader pulls it in via <script src> (which
	// works from file://, unlike fetch), so opening index.html directly
	// off disk populates the drill-downs. A missing sidecar is a no-op:
	// the page renders exactly as pr-analyzer generated it.
	SidecarFilename = "pr-scan.js"
	// sidecarGlobal is the window property the sidecar assigns and the
	// in-page loader reads. Private: the stable contract is the file name
	// + SidecarJS, not the variable name.
	sidecarGlobal = "__praEnrichment"
)

// SidecarJS serializes enrichment into the body of the sidecar file
// (SidecarFilename): a single assignment of the enrichment JSON to the
// loader's global. An enricher writes these bytes next to index.html;
// pr-analyzer never rewrites index.html and the enricher never writes
// HTML. Deterministic for identical input.
func SidecarJS(e Enrichment) ([]byte, error) {
	body, err := stdjson.MarshalIndent(e, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal enrichment: %w", err)
	}
	// Defense in depth: keep a hostile finding string from terminating
	// the surrounding <script> element. \/ decodes to / — a no-op.
	body = bytes.ReplaceAll(body, []byte("</"), []byte("<\\/"))
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "window.%s = ", sidecarGlobal)
	buf.Write(body)
	buf.WriteString(";\n")
	return buf.Bytes(), nil
}

// Render returns a complete HTML document for the given envelope. Pure
// function: no clock reads, no map iteration order surfaces, no I/O. The
// same envelope produces byte-identical output across calls — and the
// output does not depend on any enrichment: deep findings are layered in
// at view time from the sidecar (SidecarFilename), never baked into the
// page.
func Render(env rjson.Envelope) (string, error) {
	jsonBody, err := stdjson.MarshalIndent(env, "  ", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal envelope: %w", err)
	}
	// Defense in depth for the inlined-JSON path: even though
	// encoding/json never produces an unescaped </ inside a JSON
	// string, a future change to MarshalIndent or a hand-crafted
	// envelope could; substituting </ with <\/ keeps the data block
	// from being prematurely terminated by a malicious PR title and
	// is a no-op semantically (\/ decodes to /).
	jsonBody = bytes.ReplaceAll(jsonBody, []byte("</"), []byte("<\\/"))

	// Cross-PR scaling references: the largest PR fills its bar lane
	// entirely; smaller PRs scale logarithmically against it so a
	// median-sized PR stays visible next to an outlier. LOC and
	// files-changed scale independently — a PR can be small in LOC
	// but spread across many files, and vice versa.
	maxLOC := 0
	maxFiles := 0
	for _, a := range env.Analyses {
		maxLOC = max(maxLOC, a.CodeShape.LOC.Total)
		maxFiles = max(maxFiles, a.PR.ChangedFiles)
	}

	views := make([]analysisView, len(env.Analyses))
	for i, a := range env.Analyses {
		addsPct, delsPct := barPercentages(a.PR.Additions, a.PR.Deletions)
		fillPct := barFillPercentage(a.CodeShape.LOC.Total, maxLOC)
		filesFillPct := barFillPercentage(a.PR.ChangedFiles, maxFiles)
		views[i] = analysisView{
			Analysis:               a,
			BarFillStyle:           template.HTMLAttr(fmt.Sprintf(`style="width: %s%%"`, fillPct)),
			BarAddStyle:            template.HTMLAttr(fmt.Sprintf(`style="flex-basis: %s%%"`, addsPct)),
			BarDelStyle:            template.HTMLAttr(fmt.Sprintf(`style="flex-basis: %s%%"`, delsPct)),
			FilesBarFillStyle:      template.HTMLAttr(fmt.Sprintf(`style="width: %s%%"`, filesFillPct)),
			AgentConfigTouched:     len(a.CodeShape.AgentConfigPathsTouched) > 0,
			AssociationInteresting: isInterestingAssociation(a.EngineerProfile.AuthorAssociation),
			AssociationPillClass:   associationPillClass(a.EngineerProfile.AuthorAssociation),
			AuthorClass:            authorClass(a.PR.Author, a.EngineerProfile.AuthorAssociation),
		}
	}

	p := page{
		Envelope:             env,
		Count:                len(env.Analyses),
		GeneratedAtFormatted: env.GeneratedAt.UTC().Format(time.RFC3339),
		CSS:                  template.CSS(pageCSS),
		Views:                views,
	}

	tmpl := template.Must(template.New("page").Funcs(template.FuncMap{
		"join": strings.Join,
	}).Parse(pageTemplate))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	// Append the inline-data block as raw bytes — see page struct
	// doc-comment for the html/template-JS-context rationale.
	buf.WriteString("  <script type=\"application/json\" id=\"pra-data\">\n")
	buf.Write(jsonBody)
	buf.WriteString("\n  </script>\n")
	// Optional enrichment, loaded out-of-band so the page above is
	// identical whether or not anything was scanned: the sidecar
	// (SidecarFilename) is pulled in via <script src> — which works from
	// file://, unlike fetch — and the loader folds any findings into the
	// matching drill-downs. A missing sidecar leaves the page untouched.
	fmt.Fprintf(&buf, "  <script src=%q></script>\n", SidecarFilename)
	buf.WriteString("  <script>\n")
	buf.WriteString(loaderJS)
	buf.WriteString("  </script>\n")
	buf.WriteString("</body>\n</html>\n")
	return buf.String(), nil
}

// barPercentages returns the additions / deletions flex-basis
// percentages as strings rounded to one decimal — per-PR normalized
// so even a 5000-line PR shows its adds-vs-deletes ratio. When the
// PR is empty (total == 0) both segments collapse to 0 width and the
// bar's neutral background shows through.
//
// String return so the template inserts a stable token without
// locale-dependent float formatting (en-US uses "." which is what
// CSS wants — never trust LC_NUMERIC to stay there).
func barPercentages(adds, dels int) (string, string) {
	total := adds + dels
	if total <= 0 {
		return "0", "0"
	}
	addsPct := float64(adds) * 100 / float64(total)
	delsPct := float64(dels) * 100 / float64(total)
	return formatPct(addsPct), formatPct(delsPct)
}

// barFillPercentage returns the bar's fill width (0-100) for a PR
// with `total` LOC against a report whose largest PR has `max` LOC.
// Log-scaled so a 5000-LOC PR fills the lane and a 50-LOC PR is
// still visible — pure linear scaling collapses median PRs to a
// pixel when one outlier dominates the report.
//
// log(total+1) / log(max+1) handles total==1 cleanly (0.69 / log(max+1))
// without producing log(0) = -inf.
func barFillPercentage(total, max int) string {
	if max <= 0 || total <= 0 {
		return "0"
	}
	scale := math.Log(float64(total)+1) / math.Log(float64(max)+1)
	return formatPct(scale * 100)
}

func formatPct(v float64) string {
	// One decimal place, dropping a trailing ".0" so 100% reads as
	// "100" not "100.0". Pure cosmetic; the CSS value is valid either
	// way.
	s := fmt.Sprintf("%.1f", v)
	if strings.HasSuffix(s, ".0") {
		return s[:len(s)-2]
	}
	return s
}

// trustedAssociations is the small allowlist of values that carry no
// signal — the contributor has commit / merge rights on the target
// repo, so surfacing the bullet would be noise. Mirrors the rule
// applied by the CLI renderer; the two renderers are free to diverge
// later if a use case demands it.
var trustedAssociations = map[string]struct{}{
	"OWNER":        {},
	"MEMBER":       {},
	"COLLABORATOR": {},
}

func isInterestingAssociation(s string) bool {
	if s == "" {
		return false
	}
	_, trusted := trustedAssociations[s]
	return !trusted
}

// associationPillClass routes interesting buckets to color tiers:
//   - CONTRIBUTOR → success (green): positive — they've landed code here before.
//   - NONE → warning (orange): no relationship to the repo means the
//     PR comes from a fresh account or anonymous fork; review-effort
//     calibration says "pay attention" rather than "neutral".
//   - FIRST_TIME_CONTRIBUTOR / FIRST_TIMER → warning (orange): same tier as NONE.
//   - MANNEQUIN → danger (red): bot / proxy accounts, real abuse-report signal.
//   - anything else (future GitHub enum) → warning (orange) so an unknown value
//     surfaces with the conservative "worth a look" treatment.
//
// authorClass picks the summary-row author-link modifier class so a
// reviewer scanning the report can identify bots and known repeat
// contributors at a glance, without expanding the drill-down.
//
//   - "[bot]" suffix on the login → pra-pr-author-bot (yellow). Bot
//     identity wins even when the bot has a CONTRIBUTOR association:
//     a dependabot that's been around forever is still a bot, and the
//     visual cue should communicate "automation" first.
//   - CONTRIBUTOR association (human) → pra-pr-author-contributor
//     (green). Positive signal — this person has landed code here
//     before.
//   - Anything else (OWNER, MEMBER, COLLABORATOR, NONE, FIRST_TIME_*,
//     MANNEQUIN, etc.) → no modifier class. Reviewers consult the
//     drill-down for those — saves the summary row's color budget
//     for the two highest-frequency signals.
func authorClass(author, association string) string {
	if strings.HasSuffix(author, "[bot]") {
		return "pra-pr-author-bot"
	}
	if association == "CONTRIBUTOR" {
		return "pra-pr-author-contributor"
	}
	return ""
}

func associationPillClass(s string) string {
	switch s {
	case "CONTRIBUTOR":
		return "pra-pill-success"
	case "MANNEQUIN":
		return "pra-pill-danger"
	case "FIRST_TIME_CONTRIBUTOR", "FIRST_TIMER", "NONE":
		return "pra-pill-warning"
	default:
		return "pra-pill-warning"
	}
}
