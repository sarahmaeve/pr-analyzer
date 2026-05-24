// Package engineerprofile derives Engineer Profile signals about a
// PR's contributor — the second collector dimension alongside
// codeshape. It is a leaf package: it imports nothing else from
// pr-analyzer, so an embedder may use it standalone by constructing
// its own Input.
//
// Slice 4 ships the metadata-only foundation: a pass-through
// AuthorAssociation derived from data the GitHub PR-detail response
// already carries. Heavier signals (OWNERS membership,
// git log-derived prior-commit counts) consume the local clone
// directory plumbed in slice 3 and land in a later slice.
package engineerprofile

// Input is the data the orchestrator extracts from analyzer.PR and
// hands to Collect. Mirrors codeshape.Input's shape.
type Input struct {
	// AuthorAssociation is an opaque, connector-defined string
	// describing the PR author's relationship to the target
	// repository. The GitHub connector populates it from the PR's
	// author_association field; other connectors fill it with
	// whatever trust vocabulary their platform uses.
	AuthorAssociation string
}

// Signals carries the Engineer Profile signals derived from Input.
type Signals struct {
	// AuthorAssociation is the connector-supplied trust-bucket
	// string, preserved verbatim. The renderer applies its own
	// interestingness filter at display time; collection never
	// hides or rewrites the raw value.
	AuthorAssociation string
}

func Collect(in Input) Signals {
	return Signals{
		AuthorAssociation: in.AuthorAssociation,
	}
}
