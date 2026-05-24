package analyzer

import (
	"context"
	"fmt"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
)

// Option configures a single call to Analyze. The zero set of options
// preserves slice-1 behavior; Options compose additively.
type Option func(*analyzeOptions)

type analyzeOptions struct {
	config Config
}

// WithConfig threads a project-level Config into Analyze. The CodeShape
// portion drives the codeshape collector; other portions are read by
// the renderer (separately from the analyzer).
func WithConfig(c Config) Option {
	return func(o *analyzeOptions) {
		o.config = c
	}
}

func Analyze(ctx context.Context, src PRSource, ref PRRef, opts ...Option) (Analysis, error) {
	var o analyzeOptions
	for _, opt := range opts {
		opt(&o)
	}

	pr, err := src.FetchPR(ctx, ref)
	if err != nil {
		return Analysis{}, fmt.Errorf("fetch PR %s/%s#%d: %w", ref.Owner, ref.Repo, ref.Number, err)
	}

	files := make([]codeshape.File, len(pr.Files))
	for i, f := range pr.Files {
		files[i] = codeshape.File{Path: f.Path}
	}

	return Analysis{
		PR: pr,
		CodeShape: codeshape.Collect(codeshape.Input{
			Additions: pr.Additions,
			Deletions: pr.Deletions,
			Files:     files,
			Config:    o.config.CodeShape,
		}),
	}, nil
}
