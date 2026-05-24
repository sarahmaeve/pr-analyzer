// Package configfile loads a project-level pr-analyzer configuration
// from YAML on disk. The loader is intentionally lenient about shape
// mismatches: unknown keys and wrong-type values produce Warnings the
// caller can surface, while the recognized portion of the file still
// loads. Only unparseable YAML or a missing explicit-path target is a
// fatal error.
package configfile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

// DefaultConfigFilename is the bare filename the walk-up search looks
// for. Exposed so tests and integrators can reference it without
// re-declaring the literal.
const DefaultConfigFilename = "pr-analyzer.yaml"

// Warning describes a non-fatal issue the loader observed while
// decoding the YAML — e.g. an unknown key, a wrong-type value, or a
// value that fell outside its accepted range.
type Warning struct {
	// Line is the 1-based line number in the source file where the issue
	// appears. Zero if the underlying decoder did not report a location.
	Line int
	// Message is a human-readable description of the issue.
	Message string
}

// Bar-scale clamp bounds. Match render/cli's auto-scale constants.
const (
	minBarScale = 100
	maxBarScale = 1000
)

// Load reads and decodes a YAML config file. Missing-file and parse
// failures are fatal; shape mismatches (unknown keys, wrong types)
// surface via the returned Warnings, and the recognized portion of the
// file is still applied to the returned Config.
func Load(path string) (analyzer.Config, []Warning, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path is a user-supplied CLI flag; this is the intended use
	if err != nil {
		return analyzer.Config{}, nil, fmt.Errorf("read config %s: %w", path, err)
	}

	var cfg analyzer.Config
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	err = dec.Decode(&cfg)
	if errors.Is(err, io.EOF) {
		// Empty file is a valid no-op config.
		return analyzer.Config{}, nil, nil
	}

	var warnings []Warning

	// yaml.v3 returns *yaml.TypeError when KnownFields(true) trips on
	// unknown keys or wrong-type values, or when it encounters duplicate
	// keys. The struct is still populated with the fields it could
	// decode. We turn those into non-fatal Warnings and continue.
	if typeErr, ok := errors.AsType[*yaml.TypeError](err); ok {
		for _, e := range typeErr.Errors {
			warnings = append(warnings, parseTypeErrorEntry(e))
		}
	} else if err != nil {
		return analyzer.Config{}, nil, fmt.Errorf("parse config %s: %w", path, err)
	}

	warnings = append(warnings, clampBarScale(&cfg)...)

	return cfg, warnings, nil
}

// clampBarScale pins render.bar_scale into [minBarScale, maxBarScale]
// and returns a warning when it had to. Unset (zero) is left alone —
// the renderer applies its own default.
func clampBarScale(cfg *analyzer.Config) []Warning {
	v := cfg.Render.BarScale
	if v == 0 {
		return nil
	}
	if v < minBarScale {
		cfg.Render.BarScale = minBarScale
		return []Warning{{Message: fmt.Sprintf("render.bar_scale %d below minimum %d; clamped to %d", v, minBarScale, minBarScale)}}
	}
	if v > maxBarScale {
		cfg.Render.BarScale = maxBarScale
		return []Warning{{Message: fmt.Sprintf("render.bar_scale %d above maximum %d; clamped to %d", v, maxBarScale, maxBarScale)}}
	}
	return nil
}

// Discover walks up from startDir looking for DefaultConfigFilename,
// stopping at the filesystem root. The first hit is loaded and
// returned along with its path. When no file is found, the function
// returns a zero-value Config and an empty path — silently, since the
// absence of a project config is a valid state (slice-1 behavior).
func Discover(startDir string) (analyzer.Config, string, []Warning, error) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, DefaultConfigFilename)
		if _, err := os.Stat(candidate); err == nil {
			cfg, warnings, err := Load(candidate)
			return cfg, candidate, warnings, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return analyzer.Config{}, "", nil, nil
		}
		dir = parent
	}
}

// typeErrorLineRegex matches the "line N: <rest>" prefix that yaml.v3
// uses on every TypeError entry.
var typeErrorLineRegex = regexp.MustCompile(`^line (\d+):\s*(.*)$`)

func parseTypeErrorEntry(s string) Warning {
	m := typeErrorLineRegex.FindStringSubmatch(s)
	if m == nil {
		return Warning{Message: s}
	}
	line, _ := strconv.Atoi(m[1])
	return Warning{Line: line, Message: m[2]}
}
