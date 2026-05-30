package configfile_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/codeshape"
	"github.com/sarahmaeve/pr-analyzer/configfile"
	"github.com/sarahmaeve/pr-analyzer/render"
)

func TestLoad_HappyPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
		want analyzer.Config
	}{
		{
			name: "empty file → zero-value Config",
			yaml: "",
			want: analyzer.Config{},
		},
		{
			name: "render only",
			yaml: "render:\n  bar_scale: 200\n",
			want: analyzer.Config{Render: render.Config{BarScale: 200}},
		},
		{
			name: "codeshape only",
			yaml: "codeshape:\n  max_loc: 1000\n",
			want: analyzer.Config{CodeShape: codeshape.Config{MaxLOC: 1000}},
		},
		{
			name: "local_clone_dir absolute path passes through unchanged",
			yaml: "local_clone_dir: /abs/path/to/clone\n",
			want: analyzer.Config{LocalCloneDir: "/abs/path/to/clone"},
		},
		{
			name: "full config",
			yaml: `render:
  bar_scale: 200
codeshape:
  risky_paths:
    - billing
    - payments
  max_loc: 1000
  languages:
    preferred: [Go]
    allowed: [Go, TypeScript]
`,
			want: analyzer.Config{
				Render: render.Config{BarScale: 200},
				CodeShape: codeshape.Config{
					RiskyPaths: []string{"billing", "payments"},
					MaxLOC:     1000,
					Languages: codeshape.LanguageConfig{
						Preferred: []string{"Go"},
						Allowed:   []string{"Go", "TypeScript"},
					},
				},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			cfg, warnings, err := configfile.Load(path)
			if err != nil {
				t.Fatalf("Load(%q): %v", path, err)
			}
			if len(warnings) != 0 {
				t.Errorf("Load: unexpected warnings: %v", warnings)
			}
			if !reflect.DeepEqual(cfg, tc.want) {
				t.Errorf("Load() mismatch\nwant: %+v\ngot:  %+v", tc.want, cfg)
			}
		})
	}
}

func TestLoad_UnknownKeys(t *testing.T) {
	t.Parallel()

	yaml := `render:
  bar_scale: 200
codshape:
  max_loc: 1000
codeshape:
  lanugages:
    preferred: [Go]
`
	path := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, warnings, err := configfile.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The recognized portion still loads — that's the lenient contract.
	if cfg.Render.BarScale != 200 {
		t.Errorf("Render.BarScale = %d, want 200 (recognized portion must still load)", cfg.Render.BarScale)
	}

	// Each typo'd key should produce a warning that mentions the offending
	// key and a line number, so the user can find it in their file.
	var sawCodshape, sawLanugages bool
	for _, w := range warnings {
		if strings.Contains(w.Message, "codshape") {
			sawCodshape = true
		}
		if strings.Contains(w.Message, "lanugages") {
			sawLanugages = true
		}
		if w.Line <= 0 {
			t.Errorf("warning %+v has invalid line %d", w, w.Line)
		}
	}
	if !sawCodshape {
		t.Errorf("no warning mentions 'codshape'; warnings = %+v", warnings)
	}
	if !sawLanugages {
		t.Errorf("no warning mentions 'lanugages'; warnings = %+v", warnings)
	}
}

func TestLoad_TypeMismatches(t *testing.T) {
	t.Parallel()

	// max_loc and bar_scale are ints; risky_paths is []string. Wrong types
	// for each should produce a warning and leave the corresponding field
	// at its zero value while the rest of the file still loads.
	yaml := `render:
  bar_scale: "not a number"
codeshape:
  max_loc: "definitely not int"
  risky_paths: "should be a list"
  languages:
    preferred: [Go]
`
	path := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, warnings, err := configfile.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// The mismatched fields default to their zero values.
	if cfg.Render.BarScale != 0 {
		t.Errorf("Render.BarScale = %d, want 0 (type mismatch should default)", cfg.Render.BarScale)
	}
	if cfg.CodeShape.MaxLOC != 0 {
		t.Errorf("CodeShape.MaxLOC = %d, want 0", cfg.CodeShape.MaxLOC)
	}
	if len(cfg.CodeShape.RiskyPaths) != 0 {
		t.Errorf("CodeShape.RiskyPaths = %v, want empty", cfg.CodeShape.RiskyPaths)
	}

	// But the recognized portion still loads.
	if got := cfg.CodeShape.Languages.Preferred; len(got) != 1 || got[0] != "Go" {
		t.Errorf("CodeShape.Languages.Preferred = %v, want [Go]", got)
	}

	// Each mismatched field should produce a warning with a line number.
	if len(warnings) < 3 {
		t.Fatalf("warnings = %+v, want at least 3 (one per mismatched field)", warnings)
	}
	for _, w := range warnings {
		if w.Line <= 0 {
			t.Errorf("warning %+v has invalid line", w)
		}
	}
}

func TestLoad_BarScaleClamp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		yaml         string
		wantBarScale int
		wantWarning  bool
	}{
		{
			name:         "below minimum clamps to 100 with warning",
			yaml:         "render:\n  bar_scale: 50\n",
			wantBarScale: 100,
			wantWarning:  true,
		},
		{
			name:         "above maximum clamps to 1000 with warning",
			yaml:         "render:\n  bar_scale: 5000\n",
			wantBarScale: 1000,
			wantWarning:  true,
		},
		{
			name:         "exactly minimum is unchanged",
			yaml:         "render:\n  bar_scale: 100\n",
			wantBarScale: 100,
			wantWarning:  false,
		},
		{
			name:         "exactly maximum is unchanged",
			yaml:         "render:\n  bar_scale: 1000\n",
			wantBarScale: 1000,
			wantWarning:  false,
		},
		{
			name:         "in-range value is unchanged",
			yaml:         "render:\n  bar_scale: 300\n",
			wantBarScale: 300,
			wantWarning:  false,
		},
		{
			name:         "unset bar_scale leaves zero and emits no warning",
			yaml:         "render: {}\n",
			wantBarScale: 0,
			wantWarning:  false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			cfg, warnings, err := configfile.Load(path)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.Render.BarScale != tc.wantBarScale {
				t.Errorf("BarScale = %d, want %d", cfg.Render.BarScale, tc.wantBarScale)
			}
			sawClampWarning := false
			for _, w := range warnings {
				if strings.Contains(w.Message, "bar_scale") {
					sawClampWarning = true
				}
			}
			if sawClampWarning != tc.wantWarning {
				t.Errorf("clamp warning seen = %v, want %v (warnings: %+v)", sawClampWarning, tc.wantWarning, warnings)
			}
		})
	}
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		configAt    string // path (relative to tempDir) where a config file is placed; "" = none
		startSubdir string // subdir relative to tempDir from which to start walk-up
		wantFound   bool
	}{
		{
			name:        "config at startDir",
			configAt:    "pr-analyzer.yaml",
			startSubdir: "",
			wantFound:   true,
		},
		{
			name:        "config in immediate parent",
			configAt:    "pr-analyzer.yaml",
			startSubdir: "sub",
			wantFound:   true,
		},
		{
			name:        "config two levels up",
			configAt:    "pr-analyzer.yaml",
			startSubdir: "a/b",
			wantFound:   true,
		},
	}
	// "no config anywhere" lives in TestDiscover_FallsThroughToXDGAndHome,
	// where the env can be cleared deterministically — without that, a
	// dev-machine's actual ~/.config/pr-analyzer/pr-analyzer.yaml would
	// satisfy the discovery and silently flip the test's outcome.
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tempDir := t.TempDir()
			startDir := tempDir
			if tc.startSubdir != "" {
				startDir = filepath.Join(tempDir, tc.startSubdir)
				if err := os.MkdirAll(startDir, 0o755); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			}
			if tc.configAt != "" {
				path := filepath.Join(tempDir, tc.configAt)
				if err := os.WriteFile(path, []byte("render:\n  bar_scale: 200\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			cfg, foundPath, warnings, err := configfile.Discover(startDir)
			if err != nil {
				t.Fatalf("Discover: %v", err)
			}
			if len(warnings) != 0 {
				t.Errorf("Discover: unexpected warnings: %+v", warnings)
			}

			if tc.wantFound {
				if foundPath == "" {
					t.Fatalf("Discover returned empty foundPath, want non-empty")
				}
				if cfg.Render.BarScale != 200 {
					t.Errorf("BarScale = %d, want 200 (loader should have applied the file)", cfg.Render.BarScale)
				}
			} else {
				if foundPath != "" {
					t.Errorf("Discover found path %q, want empty", foundPath)
				}
				if !reflect.DeepEqual(cfg, analyzer.Config{}) {
					t.Errorf("Discover returned non-zero Config: %+v", cfg)
				}
			}
		})
	}
}

// TestDiscover_FallsThroughToXDGAndHome covers the slice-6 ladder
// past the CWD walk-up. Each sub-case calls t.Setenv to deterministically
// drive XDG_CONFIG_HOME and HOME, so the tests cannot run in parallel —
// the trade-off for testing real env handling without abstracting it.
func TestDiscover_FallsThroughToXDGAndHome(t *testing.T) {
	t.Run("XDG_CONFIG_HOME wins when set", func(t *testing.T) {
		// Start from a tempdir with no walk-up hit; the XDG path
		// must be found and applied.
		startDir := t.TempDir()
		xdgRoot := t.TempDir()
		homeRoot := t.TempDir() // also has no config — proves XDG beats HOME

		xdgConfig := filepath.Join(xdgRoot, "pr-analyzer", "pr-analyzer.yaml")
		if err := os.MkdirAll(filepath.Dir(xdgConfig), 0o755); err != nil {
			t.Fatalf("mkdir xdg: %v", err)
		}
		if err := os.WriteFile(xdgConfig, []byte("render:\n  bar_scale: 300\n"), 0o600); err != nil {
			t.Fatalf("write xdg config: %v", err)
		}

		t.Setenv("XDG_CONFIG_HOME", xdgRoot)
		t.Setenv("HOME", homeRoot)

		cfg, foundPath, _, err := configfile.Discover(startDir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if foundPath != xdgConfig {
			t.Errorf("foundPath = %q, want %q", foundPath, xdgConfig)
		}
		if cfg.Render.BarScale != 300 {
			t.Errorf("BarScale = %d, want 300 (XDG config not applied)", cfg.Render.BarScale)
		}
	})

	t.Run("HOME fallback when XDG_CONFIG_HOME unset", func(t *testing.T) {
		// Explicit empty XDG_CONFIG_HOME with HOME set → use $HOME/.config.
		startDir := t.TempDir()
		homeRoot := t.TempDir()

		homeConfig := filepath.Join(homeRoot, ".config", "pr-analyzer", "pr-analyzer.yaml")
		if err := os.MkdirAll(filepath.Dir(homeConfig), 0o755); err != nil {
			t.Fatalf("mkdir home: %v", err)
		}
		if err := os.WriteFile(homeConfig, []byte("render:\n  bar_scale: 400\n"), 0o600); err != nil {
			t.Fatalf("write home config: %v", err)
		}

		t.Setenv("XDG_CONFIG_HOME", "")
		t.Setenv("HOME", homeRoot)

		cfg, foundPath, _, err := configfile.Discover(startDir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if foundPath != homeConfig {
			t.Errorf("foundPath = %q, want %q", foundPath, homeConfig)
		}
		if cfg.Render.BarScale != 400 {
			t.Errorf("BarScale = %d, want 400 (HOME config not applied)", cfg.Render.BarScale)
		}
	})

	t.Run("walk-up beats XDG", func(t *testing.T) {
		// Both walk-up file and XDG file exist. Walk-up wins — repo-local
		// intent is more specific than user-level default.
		startDir := t.TempDir()
		walkupConfig := filepath.Join(startDir, "pr-analyzer.yaml")
		if err := os.WriteFile(walkupConfig, []byte("render:\n  bar_scale: 200\n"), 0o600); err != nil {
			t.Fatalf("write walkup config: %v", err)
		}

		xdgRoot := t.TempDir()
		xdgConfig := filepath.Join(xdgRoot, "pr-analyzer", "pr-analyzer.yaml")
		if err := os.MkdirAll(filepath.Dir(xdgConfig), 0o755); err != nil {
			t.Fatalf("mkdir xdg: %v", err)
		}
		if err := os.WriteFile(xdgConfig, []byte("render:\n  bar_scale: 700\n"), 0o600); err != nil {
			t.Fatalf("write xdg config: %v", err)
		}

		t.Setenv("XDG_CONFIG_HOME", xdgRoot)
		t.Setenv("HOME", t.TempDir())

		cfg, foundPath, _, err := configfile.Discover(startDir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if foundPath != walkupConfig {
			t.Errorf("foundPath = %q, want %q (walk-up should beat XDG)", foundPath, walkupConfig)
		}
		if cfg.Render.BarScale != 200 {
			t.Errorf("BarScale = %d, want 200 (walk-up file not applied)", cfg.Render.BarScale)
		}
	})

	t.Run("nothing found is silent", func(t *testing.T) {
		// All three sources empty/missing → zero Config, empty path, no error.
		// Without env clearing the test would silently pick up the dev
		// machine's actual ~/.config/pr-analyzer/pr-analyzer.yaml.
		startDir := t.TempDir()
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "does-not-exist"))
		t.Setenv("HOME", filepath.Join(t.TempDir(), "does-not-exist"))

		cfg, foundPath, warnings, err := configfile.Discover(startDir)
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		if foundPath != "" {
			t.Errorf("foundPath = %q, want empty", foundPath)
		}
		if !reflect.DeepEqual(cfg, analyzer.Config{}) {
			t.Errorf("Discover returned non-zero Config: %+v", cfg)
		}
		if len(warnings) != 0 {
			t.Errorf("unexpected warnings: %+v", warnings)
		}
	})
}

func TestLoad_LocalCloneDirRelativePath(t *testing.T) {
	t.Parallel()

	// A relative local_clone_dir in the YAML must resolve against the
	// YAML file's directory — that's where the user thinks of "here" when
	// they edit the file. Resolving against CWD would be surprising if
	// the user invokes the CLI from elsewhere.
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "pr-analyzer.yaml")
	if err := os.WriteFile(yamlPath, []byte("local_clone_dir: subdir\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, warnings, err := configfile.Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	want := filepath.Join(tempDir, "subdir")
	if cfg.LocalCloneDir != want {
		t.Errorf("LocalCloneDir = %q, want %q (resolved against YAML directory)", cfg.LocalCloneDir, want)
	}
}

func TestLoad_LocalCloneDirAbsolutePathUntouched(t *testing.T) {
	t.Parallel()

	// Absolute paths are NOT modified by the resolution step.
	tempDir := t.TempDir()
	yamlPath := filepath.Join(tempDir, "pr-analyzer.yaml")
	if err := os.WriteFile(yamlPath, []byte("local_clone_dir: /abs/elsewhere\n"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cfg, _, err := configfile.Load(yamlPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.LocalCloneDir != "/abs/elsewhere" {
		t.Errorf("LocalCloneDir = %q, want %q unchanged", cfg.LocalCloneDir, "/abs/elsewhere")
	}
}

func TestLoad_MissingFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "does-not-exist.yaml")
	_, _, err := configfile.Load(path)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "read config") {
		t.Errorf("error %q does not mention 'read config'", err)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q does not mention path %q", err, path)
	}
}

func TestLoad_UnparseableYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		yaml string
	}{
		{
			name: "tab character in indentation",
			yaml: "render:\n\tbar_scale: 200\n",
		},
		{
			name: "unclosed flow sequence",
			yaml: "codeshape:\n  risky_paths: [billing, payments\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "pr-analyzer.yaml")
			if err := os.WriteFile(path, []byte(tc.yaml), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			_, _, err := configfile.Load(path)
			if err == nil {
				t.Fatal("expected error for unparseable YAML, got nil")
			}
			// The error should identify both the operation that failed and the
			// file path — otherwise a user faced with one of these can't tell
			// which file is broken.
			if !strings.Contains(err.Error(), "parse config") {
				t.Errorf("error %q does not mention 'parse config'", err)
			}
			if !strings.Contains(err.Error(), path) {
				t.Errorf("error %q does not mention the file path %q", err, path)
			}
		})
	}
}
