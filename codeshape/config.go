package codeshape

// Config drives the codeshape collector's per-org behavior. Zero
// values preserve slice-1 defaults: no risky paths, no max-LOC opinion,
// no language posture.
type Config struct {
	// RiskyPaths is a list of path-segment prefixes. A pattern P matches a
	// PR file path F if P == F or P + "/" is a prefix of F. No wildcards.
	RiskyPaths []string `yaml:"risky_paths"`

	// MaxLOC is the threshold above which the renderer adds an
	// "exceeds max LOC" annotation. Zero = no opinion.
	MaxLOC int `yaml:"max_loc"`

	// Languages declares the project's posture on programming languages
	// detected in the PR. Empty lists = no opinion.
	Languages LanguageConfig `yaml:"languages"`
}

type LanguageConfig struct {
	Preferred []string `yaml:"preferred"`
	Allowed   []string `yaml:"allowed"`
}
