package cli_test

import (
	"math"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/render/cli"
)

func TestRender(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   analyzer.Analysis
		want string
	}{
		{
			name: "small PR default scale with tests and Go",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "example", Repo: "repo", Number: 113},
					Author:       "sarahmaeve",
					URL:          "https://github.com/example/repo/pull/113",
					ChangedFiles: 7,
				},
				CodeShape: codeshape.Signals{
					LOC:          codeshape.LOC{Additions: 320, Deletions: 180, Total: 500},
					TestsTouched: true,
					Languages:    []string{"Go"},
				},
			},
			want: `PR #113 sarahmaeve https://github.com/example/repo/pull/113
[++++--]
adds: 320  deletes: 180  files: 7
tests touched
no dependency manifest touched
languages: Go
`,
		},
		{
			name: "medium PR auto-scaled with scale notice and manifests",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "example", Repo: "repo", Number: 420},
					Author:       "sarahmaeve",
					URL:          "https://github.com/example/repo/pull/420",
					ChangedFiles: 47,
				},
				CodeShape: codeshape.Signals{
					LOC:              codeshape.LOC{Additions: 2500, Deletions: 1500, Total: 4000},
					TestsTouched:     true,
					ManifestsTouched: []string{"package.json", "pnpm-lock.yaml"},
					Languages:        []string{"JavaScript", "TypeScript"},
				},
			},
			want: `PR #420 sarahmaeve https://github.com/example/repo/pull/420
[+++++++++++++--------]  scale: 200 LOC/glyph
adds: 2500  deletes: 1500  files: 47
tests touched
dependency manifest touched: package.json, pnpm-lock.yaml
languages: JavaScript, TypeScript
`,
		},
		{
			name: "huge PR bar omitted",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "example", Repo: "repo", Number: 999},
					Author:       "sarahmaeve",
					URL:          "https://github.com/example/repo/pull/999",
					ChangedFiles: 312,
				},
				CodeShape: codeshape.Signals{
					LOC:              codeshape.LOC{Additions: 30000, Deletions: 20000, Total: 50000},
					TestsTouched:     true,
					ManifestsTouched: []string{"package.json", "package-lock.json"},
					Languages:        []string{"JavaScript", "JSON", "TypeScript"},
				},
			},
			want: `PR #999 sarahmaeve https://github.com/example/repo/pull/999
adds: 30000  deletes: 20000  files: 312
tests touched
dependency manifest touched: package.json, package-lock.json
languages: JavaScript, JSON, TypeScript
`,
		},
		{
			name: "zero LOC PR yields empty bar",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "example", Repo: "repo", Number: 1},
					Author:       "ghost",
					URL:          "https://github.com/example/repo/pull/1",
					ChangedFiles: 0,
				},
				CodeShape: codeshape.Signals{},
			},
			want: `PR #1 ghost https://github.com/example/repo/pull/1
[]
adds: 0  deletes: 0  files: 0
no tests touched
no dependency manifest touched
`,
		},
		{
			name: "tiny PR adds-only",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 2},
					Author:       "nobody",
					URL:          "https://github.com/x/y/pull/2",
					ChangedFiles: 1,
				},
				CodeShape: codeshape.Signals{
					LOC: codeshape.LOC{Additions: 5, Deletions: 0, Total: 5},
				},
			},
			want: `PR #2 nobody https://github.com/x/y/pull/2
[+]
adds: 5  deletes: 0  files: 1
no tests touched
no dependency manifest touched
`,
		},
		{
			name: "boundary at 30 glyphs stays at default scale",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 3},
					Author:       "boundary",
					URL:          "https://github.com/x/y/pull/3",
					ChangedFiles: 30,
				},
				CodeShape: codeshape.Signals{
					LOC:       codeshape.LOC{Additions: 1500, Deletions: 1500, Total: 3000},
					Languages: []string{"Go"},
				},
			},
			want: `PR #3 boundary https://github.com/x/y/pull/3
[` + strings.Repeat("+", 15) + strings.Repeat("-", 15) + `]
adds: 1500  deletes: 1500  files: 30
no tests touched
no dependency manifest touched
languages: Go
`,
		},
		{
			name: "31 glyphs at scale 100 triggers auto-scale",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 4},
					Author:       "trigger",
					URL:          "https://github.com/x/y/pull/4",
					ChangedFiles: 31,
				},
				CodeShape: codeshape.Signals{
					LOC:       codeshape.LOC{Additions: 1600, Deletions: 1500, Total: 3100},
					Languages: []string{"Go"},
				},
			},
			want: `PR #4 trigger https://github.com/x/y/pull/4
[` + strings.Repeat("+", 8) + strings.Repeat("-", 8) + `]  scale: 200 LOC/glyph
adds: 1600  deletes: 1500  files: 31
no tests touched
no dependency manifest touched
languages: Go
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cli.Render(tc.in)
			if got != tc.want {
				t.Errorf("Render() mismatch\n--- want ---\n%s--- got ---\n%s", tc.want, got)
			}
		})
	}
}

// TestRender_HandlesExtremeValuesWithoutPanic guards against integer
// overflow in the bar-scale math. A hostile or buggy fixture can return
// arbitrarily large additions/deletions; the renderer must not panic.
func TestRender_HandlesExtremeValuesWithoutPanic(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		additions int
		deletions int
	}{
		{"both at math.MaxInt", math.MaxInt, math.MaxInt},
		{"additions at math.MaxInt, deletions zero", math.MaxInt, 0},
		{"deletions at math.MaxInt, additions zero", 0, math.MaxInt},
		{"both near math.MaxInt (sum overflows)", math.MaxInt - 1, math.MaxInt - 1},
		{"negative inputs (defensive)", -5, -10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := analyzer.Analysis{
				PR: analyzer.PR{
					Ref:    analyzer.PRRef{Owner: "x", Repo: "y", Number: 1},
					Author: "u",
					URL:    "https://example.test/x/y/pull/1",
				},
				CodeShape: codeshape.Signals{
					LOC: codeshape.LOC{Additions: tc.additions, Deletions: tc.deletions},
				},
			}
			// The contract: do not panic. Output is best-effort for these values;
			// the bar should be omitted whenever the values exceed what scale=1000 can fit.
			got := cli.Render(in)
			if strings.HasPrefix(got, "") && got == "" {
				t.Fatal("Render returned empty string")
			}
			if !strings.Contains(got, "PR #1 u") {
				t.Errorf("missing header line:\n%s", got)
			}
		})
	}
}
