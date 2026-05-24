// Package analyzer defines the core PR types and the PRSource interface.
package analyzer

import (
	"context"
	"time"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
)

type PRRef struct {
	Owner  string
	Repo   string
	Number int
}

type PR struct {
	Ref          PRRef
	Title        string
	Author       string
	URL          string
	State        string
	Draft        bool
	BaseRef      string
	HeadRef      string
	Additions    int
	Deletions    int
	ChangedFiles int
	Labels       []string
	Files        []PRFile
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type PRFile struct {
	Path      string
	Status    string
	Additions int
	Deletions int
}

type PRSource interface {
	FetchPR(ctx context.Context, ref PRRef) (PR, error)
}

type Analysis struct {
	PR        PR
	CodeShape codeshape.Signals
}
