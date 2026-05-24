package cli_test

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/render"
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
		{
			name: "all slice-2 bullets fire in canonical order",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 999},
					Author:       "sarahmaeve",
					URL:          "https://github.com/x/y/pull/999",
					ChangedFiles: 4,
				},
				CodeShape: codeshape.Signals{
					LOC:               codeshape.LOC{Additions: 1200, Deletions: 100, Total: 1300},
					TestsTouched:      true,
					Languages:         []string{"Go", "Rust"},
					RiskyPathsTouched: []string{"billing/charge.go", "payments/refund.go"},
					ExceedsMaxLOC:     true,
					MaxLOCThreshold:   1000,
					LanguagesByPosture: codeshape.LanguagesByPosture{
						Preferred: []string{"Go"},
						Anomalous: []string{"Rust"},
					},
				},
			},
			want: `PR #999 sarahmaeve https://github.com/x/y/pull/999
[++++++++++++-]
adds: 1200  deletes: 100  files: 4
tests touched
no dependency manifest touched
languages: Go, Rust
languages preferred: Go
languages anomalous: Rust
risky paths touched: billing/charge.go, payments/refund.go
exceeds max LOC: 1300 > 1000
`,
		},
		{
			name: "languages allowed bucket renders when non-empty",
			in: analyzer.Analysis{
				PR: analyzer.PR{
					Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 42},
					Author:       "u",
					URL:          "https://github.com/x/y/pull/42",
					ChangedFiles: 2,
				},
				CodeShape: codeshape.Signals{
					LOC:       codeshape.LOC{Additions: 10, Deletions: 0, Total: 10},
					Languages: []string{"Go", "TypeScript"},
					LanguagesByPosture: codeshape.LanguagesByPosture{
						Preferred: []string{"Go"},
						Allowed:   []string{"TypeScript"},
					},
				},
			},
			want: `PR #42 u https://github.com/x/y/pull/42
[+]
adds: 10  deletes: 0  files: 2
no tests touched
no dependency manifest touched
languages: Go, TypeScript
languages preferred: Go
languages allowed: TypeScript
`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := cli.Render(tc.in, render.Config{})
			if got != tc.want {
				t.Errorf("Render() mismatch\n--- want ---\n%s--- got ---\n%s", tc.want, got)
			}
		})
	}
}

// TestRender_BarScaleOverride pins down the render.Config.BarScale path:
// when the user-supplied starting scale differs from the default, the
// bar uses it AND the scale notice fires (because scale != defaultScale).
// The renderer's auto-scale logic still applies on top of the override.
func TestRender_BarScaleOverride(t *testing.T) {
	t.Parallel()

	in := analyzer.Analysis{
		PR: analyzer.PR{
			Ref:          analyzer.PRRef{Owner: "x", Repo: "y", Number: 1},
			Author:       "u",
			URL:          "https://example.test/x/y/pull/1",
			ChangedFiles: 1,
		},
		CodeShape: codeshape.Signals{
			LOC: codeshape.LOC{Additions: 50, Deletions: 50, Total: 100},
		},
	}

	// At scale=200, ceil(50/200)=1 per side. Bar is two glyphs; notice fires.
	got := cli.Render(in, render.Config{BarScale: 200})
	want := `PR #1 u https://example.test/x/y/pull/1
[+-]  scale: 200 LOC/glyph
adds: 50  deletes: 50  files: 1
no tests touched
no dependency manifest touched
`
	if got != want {
		t.Errorf("Render() mismatch\n--- want ---\n%s--- got ---\n%s", want, got)
	}
}

// TestRender_BoundaryAndExtremeValues pins down the renderer's behavior at
// math.MaxInt (must not panic; bar must be omitted past what scale=1000
// can fit) and at negative inputs (must clamp the bar to [] but reflect
// the raw counts on the numeric line). Exact-output comparison so a
// regression to the old "WithoutPanic"-only weak assertion can't sneak
// through.
func TestRender_BoundaryAndExtremeValues(t *testing.T) {
	t.Parallel()

	const (
		number = 1
		author = "u"
		urlStr = "https://example.test/x/y/pull/1"
	)
	header := fmt.Sprintf("PR #%d %s %s\n", number, author, urlStr)
	bullets := "no tests touched\nno dependency manifest touched\n"

	cases := []struct {
		name      string
		additions int
		deletions int
		want      string
	}{
		{
			name:      "both at math.MaxInt — bar omitted",
			additions: math.MaxInt,
			deletions: math.MaxInt,
			want:      header + fmt.Sprintf("adds: %d  deletes: %d  files: 0\n", math.MaxInt, math.MaxInt) + bullets,
		},
		{
			name:      "additions at math.MaxInt, deletions zero — bar omitted",
			additions: math.MaxInt,
			deletions: 0,
			want:      header + fmt.Sprintf("adds: %d  deletes: 0  files: 0\n", math.MaxInt) + bullets,
		},
		{
			name:      "deletions at math.MaxInt, additions zero — bar omitted",
			additions: 0,
			deletions: math.MaxInt,
			want:      header + fmt.Sprintf("adds: 0  deletes: %d  files: 0\n", math.MaxInt) + bullets,
		},
		{
			name:      "both near math.MaxInt (sum would overflow) — bar omitted",
			additions: math.MaxInt - 1,
			deletions: math.MaxInt - 1,
			want:      header + fmt.Sprintf("adds: %d  deletes: %d  files: 0\n", math.MaxInt-1, math.MaxInt-1) + bullets,
		},
		{
			name:      "negative inputs clamp bar to [] but preserve numeric line",
			additions: -5,
			deletions: -10,
			want:      header + "[]\nadds: -5  deletes: -10  files: 0\n" + bullets,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := analyzer.Analysis{
				PR: analyzer.PR{
					Ref:    analyzer.PRRef{Owner: "x", Repo: "y", Number: number},
					Author: author,
					URL:    urlStr,
				},
				CodeShape: codeshape.Signals{
					LOC: codeshape.LOC{Additions: tc.additions, Deletions: tc.deletions},
				},
			}
			got := cli.Render(in, render.Config{})
			if got != tc.want {
				t.Errorf("Render() mismatch\n--- want ---\n%s--- got ---\n%s", tc.want, got)
			}
		})
	}
}
