// Package analyzer defines the core PR types and the PRSource interface.
package analyzer

import (
	"context"
	"time"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/engineerprofile"
)

type PRRef struct {
	Owner  string `json:"owner"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

type PR struct {
	Ref     PRRef  `json:"ref"`
	Title   string `json:"title"`
	Author  string `json:"author"`
	URL     string `json:"url"`
	State   string `json:"state"`
	Draft   bool   `json:"draft"`
	BaseRef string `json:"base_ref"`
	HeadRef string `json:"head_ref"`
	// BaseSHA / HeadSHA are the base/head commit SHAs as reported by the
	// connector, carried verbatim and NOT validated as well-formed object
	// names. Empty when the connector cannot supply them. They identify
	// the commit the PR proposes (HeadSHA) for downstream deep analysis; a
	// consumer that resolves HeadSHA to a git ref, path, or command
	// argument must validate its shape first — it is connector-defined,
	// opaque data, not a trusted SHA.
	BaseSHA      string    `json:"base_sha"`
	HeadSHA      string    `json:"head_sha"`
	Additions    int       `json:"additions"`
	Deletions    int       `json:"deletions"`
	ChangedFiles int       `json:"changed_files"`
	Labels       []string  `json:"labels"`
	Files        []PRFile  `json:"files"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	// AuthorAssociation is an opaque, connector-defined string that
	// describes the PR author's relationship to the target repository.
	// For the GitHub connector it carries the PR's author_association
	// value (OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR,
	// FIRST_TIME_CONTRIBUTOR, FIRST_TIMER, MANNEQUIN, NONE). Other
	// connectors are free to emit whatever trust-bucket vocabulary
	// their platform exposes; the analyzer and renderer treat the
	// value as opaque below the rendering layer.
	AuthorAssociation string `json:"author_association"`
	// AuthorType is the GitHub user object's type for the PR author:
	// "User", "Bot", or "Organization". Connectors that can't supply it
	// leave it empty. Lets consumers distinguish a human contributor
	// from an App/bot identity (dependabot[bot] etc.).
	AuthorType string `json:"author_type"`
}

type PRFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Additions int    `json:"additions"`
	Deletions int    `json:"deletions"`
}

type PRSource interface {
	FetchPR(ctx context.Context, ref PRRef) (PR, error)
	// ListOpenPRs returns the open PRs for owner/repo in the order the
	// upstream chose to list them (GitHub's default is newest-first).
	// The returned PRRefs have Owner / Repo set to the arguments and
	// Number set from the listing; connectors do not call FetchPR
	// internally here — Analyze drives the per-PR fetch separately.
	ListOpenPRs(ctx context.Context, owner, repo string) ([]PRRef, error)
}

type Analysis struct {
	PR              PR                      `json:"pr"`
	CodeShape       codeshape.Signals       `json:"code_shape"`
	EngineerProfile engineerprofile.Signals `json:"engineer_profile"`
	// Config is the org Config that produced this Analysis. It is
	// echoed back so renderers and embedders can reference the
	// configuration without re-loading it. Excluded from JSON output —
	// it's an input, not a signal, and emitting it would leak the
	// runner's local configuration into a published artifact.
	Config Config `json:"-"`
}
