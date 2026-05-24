package analyzer

import (
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/render"
)

// Config is the project-level configuration consumed by Analyze (via
// WithConfig) and the renderer. Zero values preserve slice-1 defaults
// throughout.
type Config struct {
	Render    render.Config    `yaml:"render"`
	CodeShape codeshape.Config `yaml:"codeshape"`
}
