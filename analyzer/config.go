package analyzer

import (
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/render"
)

// Config is the project-level configuration consumed by Analyze (via
// WithConfig) and the renderer. Zero values preserve slice-1 defaults
// throughout.
type Config struct {
	// LocalCloneDir is an absolute path to the PR's checked-out
	// repository on disk. pr-analyzer never clones — the caller is
	// responsible for the checkout existing. Empty string means "not
	// configured"; downstream collectors that need it error at point
	// of use.
	LocalCloneDir string           `yaml:"local_clone_dir"`
	Render        render.Config    `yaml:"render"`
	CodeShape     codeshape.Config `yaml:"codeshape"`
}
