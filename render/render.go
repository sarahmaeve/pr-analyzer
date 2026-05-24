// Package render defines interface-layer configuration shared across
// renderers. Individual renderers (e.g. render/cli) live in subpackages.
package render

// Config drives the renderer's per-project behavior. Zero values
// preserve slice-1 defaults.
type Config struct {
	// BarScale overrides the renderer's default 100 LOC/glyph. Loader
	// clamps to [100, 1000]; the renderer's auto-scale rules still apply
	// on top. Zero = use default.
	BarScale int `yaml:"bar_scale"`
}
