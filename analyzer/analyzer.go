// Package analyzer defines the core PR types and the PRSource interface.
package analyzer

import (
	"context"
	"time"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
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
	// AuthorAssociation is an opaque, connector-defined string that
	// describes the PR author's relationship to the target repository.
	// For the GitHub connector it carries the PR's author_association
	// value (OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR,
	// FIRST_TIME_CONTRIBUTOR, FIRST_TIMER, MANNEQUIN, NONE). Other
	// connectors are free to emit whatever trust-bucket vocabulary
	// their platform exposes; the analyzer and renderer treat the
	// value as opaque below the rendering layer.
	AuthorAssociation string
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
	PR              PR
	CodeShape       codeshape.Signals
	EngineerProfile engineerprofile.Signals
	// Config is the project Config that produced this Analysis. It is
	// echoed back so renderers and embedders can reference the
	// configuration without re-loading it.
	Config Config
}
