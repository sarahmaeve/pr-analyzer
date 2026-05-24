package analyzer_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

type fakeSource struct {
	pr  analyzer.PR
	err error
}

func (f fakeSource) FetchPR(_ context.Context, _ analyzer.PRRef) (analyzer.PR, error) {
	return f.pr, f.err
}

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
			ChangedFiles: 2,
			Files: []analyzer.PRFile{
				{Path: "main.go", Additions: 80, Deletions: 30},
				{Path: "main_test.go", Additions: 20, Deletions: 20},
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
	if got.PR.ChangedFiles != 2 {
		t.Errorf("PR.ChangedFiles = %d, want 2", got.PR.ChangedFiles)
	}

	wantLOC := analyzer.Analysis{}.CodeShape.LOC
	wantLOC.Additions = 100
	wantLOC.Deletions = 50
	wantLOC.Total = 150
	if got.CodeShape.LOC != wantLOC {
		t.Errorf("CodeShape.LOC = %+v, want %+v", got.CodeShape.LOC, wantLOC)
	}

	if !got.CodeShape.TestsTouched {
		t.Errorf("CodeShape.TestsTouched = false, want true (main_test.go is a Go test)")
	}

	if !slices.Equal(got.CodeShape.Languages, []string{"Go"}) {
		t.Errorf("CodeShape.Languages = %v, want [Go]", got.CodeShape.Languages)
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
