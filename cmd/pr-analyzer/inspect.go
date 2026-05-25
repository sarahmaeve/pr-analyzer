package main

import (
	"cmp"
	stdjson "encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	rhtml "github.com/sarahmaeve/pr-analyzer/render/html"
	rjson "github.com/sarahmaeve/pr-analyzer/render/json"
)

// runInspect reads an analyses.json envelope from disk and writes a
// summary report to stdout. It is the workhorse of the `inspect`
// subcommand — pure I/O + summarize().
func runInspect(cmd inspectCmd, stdout io.Writer) error {
	env, err := loadEnvelope(cmd.Path)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, summarize(env))
	return err
}

// runRenderHTML reads an analyses.json envelope from disk and writes
// the HTML report to stdout. Lets the user iterate on the renderer
// (CSS, template) against a cached scan without re-fetching from
// GitHub. Redirect stdout to capture, e.g.
//
//	pr-analyzer render-html /tmp/scan/analyses.json > /tmp/scan/index.html
func runRenderHTML(cmd renderHTMLCmd, stdout io.Writer) error {
	env, err := loadEnvelope(cmd.Path)
	if err != nil {
		return err
	}
	body, err := rhtml.Render(env)
	if err != nil {
		return fmt.Errorf("render HTML: %w", err)
	}
	_, err = io.WriteString(stdout, body)
	return err
}

// loadEnvelope is the shared read+decode path for inspect and
// render-html. Returns a friendly error if the file isn't valid
// envelope JSON.
func loadEnvelope(path string) (rjson.Envelope, error) {
	body, err := os.ReadFile(path) //nolint:gosec // G304: path is a user-supplied CLI flag; this is the intended use
	if err != nil {
		return rjson.Envelope{}, fmt.Errorf("read %s: %w", path, err)
	}
	var env rjson.Envelope
	if err := stdjson.Unmarshal(body, &env); err != nil {
		return rjson.Envelope{}, fmt.Errorf("decode %s as analyses envelope: %w", path, err)
	}
	return env, nil
}

// summarize formats an envelope as a human-readable report. Pure
// function: reads no clock, takes nothing but its argument, and
// produces deterministic output for identical inputs (every counter
// section is sorted by count-desc, ties broken alphabetically).
func summarize(env rjson.Envelope) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s — %d open PRs (generated %s, schema v%d)\n",
		env.Repo.Owner, env.Repo.Repo, len(env.Analyses),
		env.GeneratedAt.UTC().Format(time.RFC3339), env.SchemaVersion)

	if len(env.Analyses) == 0 {
		return b.String()
	}

	b.WriteString("\n")
	writeAuthorAssociations(&b, env.Analyses)
	writeLanguages(&b, env.Analyses)
	writeLOC(&b, env.Analyses)
	writeTestsTouched(&b, env.Analyses)
	writeManifests(&b, env.Analyses)
	writeAgentConfig(&b, env.Analyses)
	writeRiskyPaths(&b, env.Analyses)
	writeAnomalousLanguages(&b, env.Analyses)
	return b.String()
}

func writeAuthorAssociations(b *strings.Builder, as []analyzer.Analysis) {
	counts := make(map[string]int)
	for _, a := range as {
		k := a.PR.AuthorAssociation
		if k == "" {
			k = "(unset)"
		}
		counts[k]++
	}
	b.WriteString("Author association\n")
	writeCountedRows(b, counts)
	b.WriteString("\n")
}

func writeLanguages(b *strings.Builder, as []analyzer.Analysis) {
	counts := make(map[string]int)
	for _, a := range as {
		for _, lang := range a.CodeShape.Languages {
			counts[lang]++
		}
	}
	if len(counts) == 0 {
		return
	}
	b.WriteString("Languages (PRs touching each)\n")
	writeCountedRows(b, counts)
	b.WriteString("\n")
}

func writeLOC(b *strings.Builder, as []analyzer.Analysis) {
	totals := make([]int, len(as))
	sum := 0
	for i, a := range as {
		totals[i] = a.CodeShape.LOC.Total
		sum += totals[i]
	}
	sorted := slices.Clone(totals)
	slices.Sort(sorted)
	median := sorted[len(sorted)/2]

	b.WriteString("Lines of code (additions + deletions)\n")
	fmt.Fprintf(b, "  total:  %d\n", sum)
	fmt.Fprintf(b, "  mean:   %d\n", sum/len(as))
	fmt.Fprintf(b, "  median: %d\n", median)
	fmt.Fprintf(b, "  max:    %d\n", sorted[len(sorted)-1])

	// Top 5 by LOC. Stable sort so identical totals preserve scan order.
	top := slices.Clone(as)
	slices.SortStableFunc(top, func(a, c analyzer.Analysis) int {
		return cmp.Compare(c.CodeShape.LOC.Total, a.CodeShape.LOC.Total)
	})
	limit := min(5, len(top))
	b.WriteString("  largest:\n")
	for _, a := range top[:limit] {
		fmt.Fprintf(b, "    #%-6d %5d  %-20s  %s\n",
			a.PR.Ref.Number, a.CodeShape.LOC.Total,
			truncateField(a.PR.Author, 20),
			truncateField(a.PR.Title, 60))
	}
	b.WriteString("\n")
}

func writeTestsTouched(b *strings.Builder, as []analyzer.Analysis) {
	touched := 0
	for _, a := range as {
		if a.CodeShape.TestsTouched {
			touched++
		}
	}
	fmt.Fprintf(b, "Tests touched: %d / %d\n\n", touched, len(as))
}

func writeManifests(b *strings.Builder, as []analyzer.Analysis) {
	prCount := 0
	manifestCounts := make(map[string]int)
	for _, a := range as {
		if len(a.CodeShape.ManifestsTouched) > 0 {
			prCount++
		}
		for _, m := range a.CodeShape.ManifestsTouched {
			manifestCounts[m]++
		}
	}
	fmt.Fprintf(b, "Dependency manifests touched: %d PRs\n", prCount)
	if prCount == 0 {
		b.WriteString("\n")
		return
	}
	writeCountedRows(b, manifestCounts)
	b.WriteString("\n")
}

// writeAgentConfig always emits, even with a count of zero — the
// "no agent-config touches" all-clear is a load-bearing signal for
// the Trapdoor-style threat class. Suppressing it would hide a
// no-news-is-good-news result.
func writeAgentConfig(b *strings.Builder, as []analyzer.Analysis) {
	var hits []agentConfigHit
	for _, a := range as {
		if len(a.CodeShape.AgentConfigPathsTouched) > 0 {
			hits = append(hits, agentConfigHit{
				number:            a.PR.Ref.Number,
				author:            a.PR.Author,
				authorAssociation: a.PR.AuthorAssociation,
				paths:             a.CodeShape.AgentConfigPathsTouched,
			})
		}
	}
	fmt.Fprintf(b, "Agent-config files touched: %d PRs\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(b, "  #%-6d %-20s [%s]: %s\n",
			h.number, truncateField(h.author, 20), h.authorAssociation, strings.Join(h.paths, ", "))
	}
	b.WriteString("\n")
}

type agentConfigHit struct {
	number            int
	author            string
	authorAssociation string
	paths             []string
}

func writeRiskyPaths(b *strings.Builder, as []analyzer.Analysis) {
	count := 0
	for _, a := range as {
		if len(a.CodeShape.RiskyPathsTouched) > 0 {
			count++
		}
	}
	if count == 0 {
		return
	}
	fmt.Fprintf(b, "Risky paths touched: %d PRs\n", count)
	for _, a := range as {
		if len(a.CodeShape.RiskyPathsTouched) > 0 {
			fmt.Fprintf(b, "  #%-6d %s\n", a.PR.Ref.Number, strings.Join(a.CodeShape.RiskyPathsTouched, ", "))
		}
	}
	b.WriteString("\n")
}

func writeAnomalousLanguages(b *strings.Builder, as []analyzer.Analysis) {
	type hit struct {
		number    int
		anomalous []string
	}
	var hits []hit
	for _, a := range as {
		if len(a.CodeShape.LanguagesByPosture.Anomalous) > 0 {
			hits = append(hits, hit{number: a.PR.Ref.Number, anomalous: a.CodeShape.LanguagesByPosture.Anomalous})
		}
	}
	if len(hits) == 0 {
		return
	}
	fmt.Fprintf(b, "Anomalous languages (vs project posture): %d PRs\n", len(hits))
	for _, h := range hits {
		fmt.Fprintf(b, "  #%-6d %s\n", h.number, strings.Join(h.anomalous, ", "))
	}
	b.WriteString("\n")
}

// writeCountedRows emits a key/count table sorted by count descending,
// alphabetical for ties. Determinism here is what makes the whole
// summarize() output stable across runs.
func writeCountedRows(b *strings.Builder, counts map[string]int) {
	type row struct {
		key   string
		count int
	}
	rows := make([]row, 0, len(counts))
	for k, v := range counts {
		rows = append(rows, row{k, v})
	}
	slices.SortFunc(rows, func(a, c row) int {
		if a.count != c.count {
			return cmp.Compare(c.count, a.count)
		}
		return cmp.Compare(a.key, c.key)
	})
	// Column width adapts to the longest key, capped so a pathological
	// long string (250-char manifest path) doesn't shove the count past
	// the terminal edge. The cap also handles the common case (most
	// keys fit comfortably) without padding everything to 30+ columns.
	const (
		minWidth = 22
		maxWidth = 60
	)
	width := minWidth
	for k := range counts {
		if w := len(k); w > width {
			width = w
		}
	}
	if width > maxWidth {
		width = maxWidth
	}
	for _, r := range rows {
		fmt.Fprintf(b, "  %-*s %4d\n", width, r.key, r.count)
	}
}

// truncateField clips s to maxLen runes, appending an ellipsis if the
// input was longer. Used to keep the LOC top-5 table from wrapping on
// PRs with novella-length titles.
func truncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen < 1 {
		return ""
	}
	return s[:maxLen-1] + "…"
}
