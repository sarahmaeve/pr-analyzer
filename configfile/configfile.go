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
	resolveLocalCloneDir(&cfg, path)

	return cfg, warnings, nil
}

// resolveLocalCloneDir converts a relative LocalCloneDir into an
// absolute path, resolved against the directory containing the YAML
// file. Absolute paths and empty strings are left untouched. The
// loader does not stat the result — existence validation is the
// downstream collector's responsibility.
func resolveLocalCloneDir(cfg *analyzer.Config, yamlPath string) {
	if cfg.LocalCloneDir == "" || filepath.IsAbs(cfg.LocalCloneDir) {
		return
	}
	cfg.LocalCloneDir = filepath.Join(filepath.Dir(yamlPath), cfg.LocalCloneDir)
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

// Discover resolves the org config via the slice-6 ladder:
//
//  1. CWD walk-up — walk from startDir upward, looking for
//     DefaultConfigFilename, stopping at the filesystem root.
//  2. $XDG_CONFIG_HOME/pr-analyzer/pr-analyzer.yaml, if set.
//  3. $HOME/.config/pr-analyzer/pr-analyzer.yaml.
//
// The first source that exists wins; later sources are not
// consulted. When nothing is found the function returns a
// zero-value Config and an empty path — silently, since the absence
// of an org config is a valid state (slice-1 behavior).
//
// The walk-up takes precedence over the user-level paths because
// repo-local intent is more specific than user-level default: a
// contributor inside an OSS checkout that ships its own
// pr-analyzer.yaml should see that project's rules regardless of
// whatever they have at ~/.config.
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
			break
		}
		dir = parent
	}
	if userPath := userConfigPath(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME")); userPath != "" {
		if _, err := os.Stat(userPath); err == nil {
			cfg, warnings, err := Load(userPath)
			return cfg, userPath, warnings, err
		}
	}
	return analyzer.Config{}, "", nil, nil
}

// userConfigPath returns the user-level org-config path the
// discovery ladder should try, or "" when neither XDG_CONFIG_HOME
// nor HOME is set. Pure over its two arguments so tests can drive
// each branch without mutating process env.
//
// Follows the XDG Base Directory Specification: when
// XDG_CONFIG_HOME is unset, the implied default is $HOME/.config.
func userConfigPath(xdgConfigHome, home string) string {
	if xdgConfigHome != "" {
		return filepath.Join(xdgConfigHome, "pr-analyzer", DefaultConfigFilename)
	}
	if home != "" {
		return filepath.Join(home, ".config", "pr-analyzer", DefaultConfigFilename)
	}
	return ""
}

// typeErrorLineRegex matches the "line N: <rest>" prefix that yaml.v3
// uses on every TypeError entry.
var typeErrorLineRegex = regexp.MustCompile(`^line (\d+):\s*(.*)$`)

func parseTypeErrorEntry(s string) Warning {
	m := typeErrorLineRegex.FindStringSubmatch(s)
	if m == nil {
		return Warning{Message: s}
	}
	line, _ := strconv.Atoi(m[1]) // safe: regex \d+ guarantees the capture is digits-only
	return Warning{Line: line, Message: m[2]}
}
