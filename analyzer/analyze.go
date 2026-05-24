package analyzer

import (
	"context"
	"fmt"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
)

func Analyze(ctx context.Context, src PRSource, ref PRRef) (Analysis, error) {
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
		}),
	}, nil
}
