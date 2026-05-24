package analyzer_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
)

type fakeSource struct {
	pr  analyzer.PR
	err error
}

func (f fakeSource) FetchPR(_ context.Context, _ analyzer.PRRef) (analyzer.PR, error) {
	return f.pr, f.err
}

// TestAnalyze_translatesPRtoInputAndPopulatesCodeShape exercises every
// codeshape signal that the orchestrator is responsible for flowing
// through. A regression in the PR→codeshape.Input translation (e.g.
// dropping the Files slice, swapping additions/deletions) must surface
// here.
func TestAnalyze_translatesPRtoInputAndPopulatesCodeShape(t *testing.T) {
	t.Parallel()

	ref := analyzer.PRRef{Owner: "x", Repo: "y", Number: 1}
	src := fakeSource{
		pr: analyzer.PR{
			Ref:          ref,
			Author:       "u",
			URL:          "https://github.com/x/y/pull/1",
			Additions:    100,
			Deletions:    50,
			ChangedFiles: 3,
			Files: []analyzer.PRFile{
				{Path: "main.go", Additions: 80, Deletions: 30},
				{Path: "main_test.go", Additions: 20, Deletions: 20},
				{Path: "go.mod", Additions: 1, Deletions: 0},
			},
		},
	}

	got, err := analyzer.Analyze(context.Background(), src, ref)
	if err != nil {
		t.Fatalf("Analyze() unexpected error: %v", err)
	}

	if got.PR.Author != "u" {
		t.Errorf("PR.Author = %q, want %q", got.PR.Author, "u")
	}
	if got.PR.ChangedFiles != 3 {
		t.Errorf("PR.ChangedFiles = %d, want 3", got.PR.ChangedFiles)
	}

	wantLOC := codeshape.LOC{Additions: 100, Deletions: 50, Total: 150}
	if got.CodeShape.LOC != wantLOC {
		t.Errorf("CodeShape.LOC = %+v, want %+v", got.CodeShape.LOC, wantLOC)
	}

	if !got.CodeShape.TestsTouched {
		t.Errorf("CodeShape.TestsTouched = false, want true (main_test.go is a Go test)")
	}

	if !slices.Equal(got.CodeShape.ManifestsTouched, []string{"go.mod"}) {
		t.Errorf("CodeShape.ManifestsTouched = %v, want [go.mod]", got.CodeShape.ManifestsTouched)
	}

	if !slices.Equal(got.CodeShape.Languages, []string{"Go"}) {
		t.Errorf("CodeShape.Languages = %v, want [Go]", got.CodeShape.Languages)
	}
}

func TestAnalyze_WithConfig_flowsThroughToCodeShape(t *testing.T) {
	t.Parallel()

	ref := analyzer.PRRef{Owner: "x", Repo: "y", Number: 1}
	src := fakeSource{
		pr: analyzer.PR{
			Ref:          ref,
			Author:       "u",
			URL:          "https://github.com/x/y/pull/1",
			Additions:    1500,
			Deletions:    100,
			ChangedFiles: 3,
			Files: []analyzer.PRFile{
				{Path: "billing/charge.go"},
				{Path: "main.go"},
				{Path: "lib.rs"},
			},
		},
	}
	cfg := analyzer.Config{
		CodeShape: codeshape.Config{
			RiskyPaths: []string{"billing"},
			MaxLOC:     1000,
			Languages: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
			},
		},
	}

	got, err := analyzer.Analyze(context.Background(), src, ref, analyzer.WithConfig(cfg))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}

	if !slices.Equal(got.CodeShape.RiskyPathsTouched, []string{"billing/charge.go"}) {
		t.Errorf("RiskyPathsTouched = %v, want [billing/charge.go]", got.CodeShape.RiskyPathsTouched)
	}
	if !got.CodeShape.ExceedsMaxLOC {
		t.Errorf("ExceedsMaxLOC = false, want true (1600 > 1000)")
	}
	if got.CodeShape.MaxLOCThreshold != 1000 {
		t.Errorf("MaxLOCThreshold = %d, want 1000", got.CodeShape.MaxLOCThreshold)
	}
	if !slices.Equal(got.CodeShape.LanguagesByPosture.Preferred, []string{"Go"}) {
		t.Errorf("LanguagesByPosture.Preferred = %v, want [Go]", got.CodeShape.LanguagesByPosture.Preferred)
	}
	if !slices.Equal(got.CodeShape.LanguagesByPosture.Anomalous, []string{"Rust"}) {
		t.Errorf("LanguagesByPosture.Anomalous = %v, want [Rust]", got.CodeShape.LanguagesByPosture.Anomalous)
	}
}

// TestAnalyze_WithConfig_LocalCloneDirFlowsThrough pins the slice-3
// plumbing: a LocalCloneDir set on the Config given to WithConfig must
// reach Analysis.Config.LocalCloneDir unchanged. No collector reads it
// yet — this test exists so a regression in Analyze's option handling
// (e.g. dropping the field while plumbing slice-4's engineer profile)
// fails loud.
func TestAnalyze_WithConfig_LocalCloneDirFlowsThrough(t *testing.T) {
	t.Parallel()

	ref := analyzer.PRRef{Owner: "x", Repo: "y", Number: 1}
	src := fakeSource{pr: analyzer.PR{Ref: ref}}

	got, err := analyzer.Analyze(context.Background(), src, ref,
		analyzer.WithConfig(analyzer.Config{LocalCloneDir: "/some/path"}))
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got.Config.LocalCloneDir != "/some/path" {
		t.Errorf("Analysis.Config.LocalCloneDir = %q, want %q", got.Config.LocalCloneDir, "/some/path")
	}
}

func TestAnalyze_propagatesSourceError(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("fetch failed")
	src := fakeSource{err: sentinel}

	_, err := analyzer.Analyze(context.Background(), src, analyzer.PRRef{Owner: "x", Repo: "y", Number: 1})
	if !errors.Is(err, sentinel) {
		t.Errorf("Analyze() error = %v, want wrap of %v", err, sentinel)
	}
}
