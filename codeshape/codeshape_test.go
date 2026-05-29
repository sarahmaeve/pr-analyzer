package codeshape_test

import (
	"slices"
	"testing"

	"github.com/sarahmaeve/pr-analyzer/codeshape"
)

func TestCollect_LOC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   codeshape.Input
		want codeshape.LOC
	}{
		{
			name: "mixed adds and deletes",
			in:   codeshape.Input{Additions: 320, Deletions: 180},
			want: codeshape.LOC{Additions: 320, Deletions: 180, Total: 500},
		},
		{
			name: "zero LOC",
			in:   codeshape.Input{},
			want: codeshape.LOC{},
		},
		{
			name: "additions only",
			in:   codeshape.Input{Additions: 100},
			want: codeshape.LOC{Additions: 100, Total: 100},
		},
		{
			name: "deletions only",
			in:   codeshape.Input{Deletions: 50},
			want: codeshape.LOC{Deletions: 50, Total: 50},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codeshape.Collect(tc.in).LOC
			if got != tc.want {
				t.Errorf("Collect(%+v).LOC = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

func TestCollect_TestsTouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []codeshape.File
		want  bool
	}{
		{"no files", nil, false},
		{"go test suffix", []codeshape.File{{Path: "pkg/foo_test.go"}}, true},
		{"python test_ prefix", []codeshape.File{{Path: "src/test_module.py"}}, true},
		{"python _test suffix", []codeshape.File{{Path: "src/module_test.py"}}, true},
		{"ruby _spec suffix", []codeshape.File{{Path: "lib/widget_spec.rb"}}, true},
		{"ruby _test suffix", []codeshape.File{{Path: "lib/widget_test.rb"}}, true},
		{"jest .test. middle", []codeshape.File{{Path: "src/foo.test.js"}}, true},
		{"karma .spec. middle", []codeshape.File{{Path: "src/bar.spec.ts"}}, true},
		{"tests/ directory at root", []codeshape.File{{Path: "tests/something.txt"}}, true},
		{"test/ directory mid-path", []codeshape.File{{Path: "pkg/test/main.py"}}, true},
		{"spec/ directory mid-path", []codeshape.File{{Path: "src/spec/util.rb"}}, true},
		{"non-test go file", []codeshape.File{{Path: "pkg/foo.go"}}, false},
		{"file named test.go alone (Go convention requires _test.go)", []codeshape.File{{Path: "test.go"}}, false},
		{"file named tests.go alone", []codeshape.File{{Path: "tests.go"}}, false},
		{"testing-substring is not a test path", []codeshape.File{{Path: "src/lib.testing.go"}}, false},
		{"mix with at least one test", []codeshape.File{
			{Path: "pkg/foo.go"},
			{Path: "pkg/foo_test.go"},
		}, true},
		{"mix without test", []codeshape.File{
			{Path: "pkg/foo.go"},
			{Path: "pkg/bar.go"},
		}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codeshape.Collect(codeshape.Input{Files: tc.files}).TestsTouched
			if got != tc.want {
				t.Errorf("Collect(files=%+v).TestsTouched = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}

func TestCollect_ManifestsTouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []codeshape.File
		want  []string
	}{
		{"no files", nil, nil},
		{"no manifests", []codeshape.File{{Path: "src/foo.go"}}, nil},
		{"go.mod at root", []codeshape.File{{Path: "go.mod"}}, []string{"go.mod"}},
		{"go.sum at root", []codeshape.File{{Path: "go.sum"}}, []string{"go.sum"}},
		{"package.json", []codeshape.File{{Path: "package.json"}}, []string{"package.json"}},
		{"package-lock.json", []codeshape.File{{Path: "package-lock.json"}}, []string{"package-lock.json"}},
		{"pnpm-lock.yaml", []codeshape.File{{Path: "pnpm-lock.yaml"}}, []string{"pnpm-lock.yaml"}},
		{"yarn.lock", []codeshape.File{{Path: "yarn.lock"}}, []string{"yarn.lock"}},
		{"requirements.txt", []codeshape.File{{Path: "requirements.txt"}}, []string{"requirements.txt"}},
		{"Pipfile.lock", []codeshape.File{{Path: "Pipfile.lock"}}, []string{"Pipfile.lock"}},
		{"Cargo.toml", []codeshape.File{{Path: "Cargo.toml"}}, []string{"Cargo.toml"}},
		{"Cargo.lock", []codeshape.File{{Path: "Cargo.lock"}}, []string{"Cargo.lock"}},
		{"Gemfile", []codeshape.File{{Path: "Gemfile"}}, []string{"Gemfile"}},
		{"Gemfile.lock", []codeshape.File{{Path: "Gemfile.lock"}}, []string{"Gemfile.lock"}},
		{"manifest in subdir keeps full path", []codeshape.File{{Path: "tools/go.mod"}}, []string{"tools/go.mod"}},
		{"multiple manifests preserve file-list order", []codeshape.File{
			{Path: "go.mod"},
			{Path: "src/foo.go"},
			{Path: "tools/go.sum"},
		}, []string{"go.mod", "tools/go.sum"}},
		{"similar-named non-manifest", []codeshape.File{{Path: "src/go.mod.bak"}}, nil},
		{"case sensitive (gemfile is not Gemfile)", []codeshape.File{{Path: "gemfile"}}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codeshape.Collect(codeshape.Input{Files: tc.files}).ManifestsTouched
			if !slices.Equal(got, tc.want) {
				t.Errorf("Collect(files=%+v).ManifestsTouched = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}

func TestCollect_RiskyPathsTouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		files      []codeshape.File
		riskyPaths []string
		want       []string
	}{
		{
			name:       "no config returns nil",
			files:      []codeshape.File{{Path: "billing/foo.go"}},
			riskyPaths: nil,
			want:       nil,
		},
		{
			name:       "configured but no match returns nil",
			files:      []codeshape.File{{Path: "src/foo.go"}},
			riskyPaths: []string{"billing"},
			want:       nil,
		},
		{
			name:       "single prefix match",
			files:      []codeshape.File{{Path: "billing/foo.go"}},
			riskyPaths: []string{"billing"},
			want:       []string{"billing/foo.go"},
		},
		{
			name:       "exact path match (no trailing slash needed)",
			files:      []codeshape.File{{Path: "billing"}},
			riskyPaths: []string{"billing"},
			want:       []string{"billing"},
		},
		{
			name:       "prefix collision: billings is not billing",
			files:      []codeshape.File{{Path: "billings/foo.go"}},
			riskyPaths: []string{"billing"},
			want:       nil,
		},
		{
			name:       "multi-segment pattern",
			files:      []codeshape.File{{Path: "auth/identity/jwt.go"}},
			riskyPaths: []string{"auth/identity"},
			want:       []string{"auth/identity/jwt.go"},
		},
		{
			name:       "multi-segment near miss",
			files:      []codeshape.File{{Path: "auth/identity2/jwt.go"}},
			riskyPaths: []string{"auth/identity"},
			want:       nil,
		},
		{
			name: "multiple matches preserve file-list order",
			files: []codeshape.File{
				{Path: "payments/x.go"},
				{Path: "src/y.go"},
				{Path: "billing/z.go"},
			},
			riskyPaths: []string{"billing", "payments"},
			want:       []string{"payments/x.go", "billing/z.go"},
		},
		{
			name: "file matching two patterns is reported once",
			files: []codeshape.File{
				{Path: "billing/charge.go"},
			},
			riskyPaths: []string{"billing", "billing/charge.go"},
			want:       []string{"billing/charge.go"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := codeshape.Input{
				Files:  tc.files,
				Config: codeshape.Config{RiskyPaths: tc.riskyPaths},
			}
			got := codeshape.Collect(in).RiskyPathsTouched
			if !slices.Equal(got, tc.want) {
				t.Errorf("RiskyPathsTouched = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestMatchesRiskyPath pins the exported single-path matcher's contract —
// the rule shared with importers like signatory's pr-scan. Prefix match is
// path-segment-bounded (a pattern matches the dir and everything beneath,
// not a sibling sharing a name prefix); empty patterns are ignored.
func TestMatchesRiskyPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path     string
		patterns []string
		want     bool
	}{
		{"billing/charge.go", []string{"billing"}, true},   // dir prefix
		{"billing", []string{"billing"}, true},             // exact
		{"billingsystem/x.go", []string{"billing"}, false}, // segment-bounded: not a sibling
		{"src/foo.go", []string{"billing"}, false},         // no match
		{"a/b/c.go", []string{"a/b"}, true},                // nested dir prefix
		{"billing/x.go", nil, false},                       // no patterns
		{"billing/x.go", []string{""}, false},              // empty pattern ignored
	}
	for _, tc := range tests {
		if got := codeshape.MatchesRiskyPath(tc.path, tc.patterns); got != tc.want {
			t.Errorf("MatchesRiskyPath(%q, %v) = %v, want %v", tc.path, tc.patterns, got, tc.want)
		}
	}
}

// TestDetectLanguages_and_BucketLanguages pins the exported language
// weighting shared with importers (signatory's pr-scan): path-based
// detection, and posture bucketing that excludes markup and flags an
// unlisted programming language as Anomalous.
func TestDetectLanguages_and_BucketLanguages(t *testing.T) {
	t.Parallel()
	files := []codeshape.File{
		{Path: "cmd/main.go"},               // Go
		{Path: "crates/x/src/lib.rs"},       // Rust
		{Path: "README.md"},                 // Markdown (markup, excluded)
		{Path: ".github/workflows/ci.yaml"}, // YAML (markup, excluded)
	}
	detected := codeshape.DetectLanguages(files)
	// Sorted, unique; includes markup in raw detection.
	if !slices.Contains(detected, "Go") || !slices.Contains(detected, "Rust") {
		t.Fatalf("DetectLanguages missing Go/Rust: %v", detected)
	}

	posture := codeshape.BucketLanguages(detected, codeshape.LanguageConfig{Preferred: []string{"Go"}})
	if !slices.Equal(posture.Preferred, []string{"Go"}) {
		t.Errorf("Preferred = %v, want [Go]", posture.Preferred)
	}
	if !slices.Equal(posture.Anomalous, []string{"Rust"}) {
		t.Errorf("Anomalous = %v, want [Rust] (markup excluded, unlisted prog lang flagged)", posture.Anomalous)
	}

	// No posture configured → no opinion.
	if got := codeshape.BucketLanguages(detected, codeshape.LanguageConfig{}); len(got.Anomalous) != 0 {
		t.Errorf("zero config must yield no anomalous langs, got %v", got.Anomalous)
	}
}

// TestTouchedManifests pins the exported manifest catalog matcher shared
// with importers (signatory's pr-scan): basename match against known
// dependency manifests/lockfiles, order-preserving, subdir-aware.
func TestTouchedManifests(t *testing.T) {
	t.Parallel()
	files := []codeshape.File{
		{Path: "go.mod"},
		{Path: "src/app.go"},
		{Path: "frontend/package-lock.json"},
		{Path: "crates/x/Cargo.toml"},
		{Path: "README.md"},
		{Path: "go.mod.bak"}, // not a manifest (basename differs)
	}
	got := codeshape.TouchedManifests(files)
	want := []string{"go.mod", "frontend/package-lock.json", "crates/x/Cargo.toml"}
	if !slices.Equal(got, want) {
		t.Errorf("TouchedManifests = %v, want %v", got, want)
	}
}

func TestCollect_AgentConfigPathsTouched(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []codeshape.File
		want  []string
	}{
		{"no files", nil, nil},
		{"no agent config files", []codeshape.File{{Path: "src/foo.go"}}, nil},
		{".cursorrules at root", []codeshape.File{{Path: ".cursorrules"}}, []string{".cursorrules"}},
		{"CLAUDE.md at root", []codeshape.File{{Path: "CLAUDE.md"}}, []string{"CLAUDE.md"}},
		{"CLAUDE.md in subdir", []codeshape.File{{Path: "docs/CLAUDE.md"}}, []string{"docs/CLAUDE.md"}},
		{"AGENTS.md at root", []codeshape.File{{Path: "AGENTS.md"}}, []string{"AGENTS.md"}},
		{"GEMINI.md at root", []codeshape.File{{Path: "GEMINI.md"}}, []string{"GEMINI.md"}},
		{".windsurfrules at root", []codeshape.File{{Path: ".windsurfrules"}}, []string{".windsurfrules"}},
		{".aider.conf.yml at root", []codeshape.File{{Path: ".aider.conf.yml"}}, []string{".aider.conf.yml"}},
		{".aider.conf.yaml at root", []codeshape.File{{Path: ".aider.conf.yaml"}}, []string{".aider.conf.yaml"}},
		{".claude/ dir at root", []codeshape.File{{Path: ".claude/settings.json"}}, []string{".claude/settings.json"}},
		{".cursor/ dir at root", []codeshape.File{{Path: ".cursor/rules"}}, []string{".cursor/rules"}},
		{".aider/ dir at root", []codeshape.File{{Path: ".aider/config.yml"}}, []string{".aider/config.yml"}},
		{".zed/ dir at root", []codeshape.File{{Path: ".zed/settings.json"}}, []string{".zed/settings.json"}},
		{".codex/ dir at root", []codeshape.File{{Path: ".codex/instructions.md"}}, []string{".codex/instructions.md"}},
		{".continue/ dir at root", []codeshape.File{{Path: ".continue/config.json"}}, []string{".continue/config.json"}},
		{".windsurf/ dir at root", []codeshape.File{{Path: ".windsurf/rules.md"}}, []string{".windsurf/rules.md"}},
		{".gemini/ dir at root", []codeshape.File{{Path: ".gemini/config.yaml"}}, []string{".gemini/config.yaml"}},
		{".gemini file at root (alt to GEMINI.md)", []codeshape.File{{Path: ".gemini"}}, []string{".gemini"}},
		{".github/copilot-instructions.md canonical Copilot path", []codeshape.File{{Path: ".github/copilot-instructions.md"}}, []string{".github/copilot-instructions.md"}},
		{"copilot-instructions.md basename match anywhere", []codeshape.File{{Path: "docs/copilot-instructions.md"}}, []string{"docs/copilot-instructions.md"}},
		{".github/instructions/* path-scoped Copilot file", []codeshape.File{{Path: ".github/instructions/api.instructions.md"}}, []string{".github/instructions/api.instructions.md"}},
		{".instructions.md suffix match anywhere", []codeshape.File{{Path: "src/auth.instructions.md"}}, []string{"src/auth.instructions.md"}},
		{".cursor/ dir nested mid-path", []codeshape.File{{Path: "frontend/.cursor/rules"}}, []string{"frontend/.cursor/rules"}},
		{".claude/ dir nested deep", []codeshape.File{{Path: "apps/web/.claude/settings.local.json"}}, []string{"apps/web/.claude/settings.local.json"}},
		{
			name: "Trapdoor canonical: PR adds .cursorrules and CLAUDE.md together",
			files: []codeshape.File{
				{Path: ".cursorrules"},
				{Path: "CLAUDE.md"},
			},
			want: []string{".cursorrules", "CLAUDE.md"},
		},
		{
			name: "mix preserves file-list order",
			files: []codeshape.File{
				{Path: "src/foo.go"},
				{Path: ".cursor/rules"},
				{Path: "src/bar.go"},
				{Path: "CLAUDE.md"},
			},
			want: []string{".cursor/rules", "CLAUDE.md"},
		},
		{
			name:  "case sensitive — claude.md is not CLAUDE.md",
			files: []codeshape.File{{Path: "claude.md"}},
			want:  nil,
		},
		{
			name:  "case sensitive — .Cursor is not .cursor",
			files: []codeshape.File{{Path: ".Cursor/rules"}},
			want:  nil,
		},
		{
			name:  "similar-named non-match — .cursorrules.bak",
			files: []codeshape.File{{Path: ".cursorrules.bak"}},
			want:  nil,
		},
		{
			name:  "similar-named non-match — prefix only",
			files: []codeshape.File{{Path: ".claude.bak/settings.json"}},
			want:  nil,
		},
		{
			name:  ".vscode is not in catalog (general IDE, not AI-agent)",
			files: []codeshape.File{{Path: ".vscode/settings.json"}},
			want:  nil,
		},
		{
			name:  ".idea is not in catalog",
			files: []codeshape.File{{Path: ".idea/workspace.xml"}},
			want:  nil,
		},
		{
			name:  "bare instructions.md is not a Copilot instruction file (suffix requires prefix)",
			files: []codeshape.File{{Path: "instructions.md"}},
			want:  nil,
		},
		{
			name:  "instruction.md singular is not the catalog compound suffix",
			files: []codeshape.File{{Path: "api.instruction.md"}},
			want:  nil,
		},
		{
			name:  "literal .instructions.md filename does not match suffix without prefix",
			files: []codeshape.File{{Path: ".instructions.md"}},
			want:  nil,
		},
		{
			name:  ".github is not blanket-matched (only specific Copilot files within)",
			files: []codeshape.File{{Path: ".github/workflows/ci.yml"}},
			want:  nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codeshape.Collect(codeshape.Input{Files: tc.files}).AgentConfigPathsTouched
			if !slices.Equal(got, tc.want) {
				t.Errorf("Collect(files=%+v).AgentConfigPathsTouched = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}

func TestCollect_LanguagesByPosture(t *testing.T) {
	t.Parallel()

	type want struct {
		preferred []string
		allowed   []string
		anomalous []string
	}

	tests := []struct {
		name   string
		files  []codeshape.File
		config codeshape.LanguageConfig
		want   want
	}{
		{
			name:   "empty config returns zero-value posture (no opinion)",
			files:  []codeshape.File{{Path: "main.go"}, {Path: "lib.ts"}},
			config: codeshape.LanguageConfig{},
			want:   want{},
		},
		{
			name:  "preferred and allowed buckets",
			files: []codeshape.File{{Path: "main.go"}, {Path: "lib.ts"}},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
				Allowed:   []string{"Go", "TypeScript"},
			},
			want: want{
				preferred: []string{"Go"},
				allowed:   []string{"TypeScript"},
			},
		},
		{
			name:  "detected programming language outside both lists is anomalous",
			files: []codeshape.File{{Path: "main.go"}, {Path: "lib.rs"}},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
			},
			want: want{
				preferred: []string{"Go"},
				anomalous: []string{"Rust"},
			},
		},
		{
			name: "non-programming languages (Markdown, YAML) never anomalous",
			files: []codeshape.File{
				{Path: "main.go"},
				{Path: "README.md"},
				{Path: ".github/workflows/ci.yml"},
			},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
			},
			want: want{
				preferred: []string{"Go"},
				// Markdown and YAML are non-programming; never anomalous.
			},
		},
		{
			name: "shell script anywhere counts as Shell (no CI-path exemption in slice 2)",
			files: []codeshape.File{
				{Path: "main.go"},
				{Path: ".github/workflows/build.sh"},
			},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
			},
			want: want{
				preferred: []string{"Go"},
				anomalous: []string{"Shell"},
			},
		},
		{
			name: "Lua buckets as a programming language (anomalous when not in posture)",
			files: []codeshape.File{
				{Path: "main.go"},
				{Path: "kong/plugins/foo/handler.lua"},
			},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
			},
			want: want{
				preferred: []string{"Go"},
				anomalous: []string{"Lua"},
			},
		},
		{
			name:  "language in preferred but not allowed still buckets as preferred",
			files: []codeshape.File{{Path: "main.go"}},
			config: codeshape.LanguageConfig{
				Preferred: []string{"Go"},
				Allowed:   []string{"TypeScript"},
			},
			want: want{
				preferred: []string{"Go"},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := codeshape.Input{
				Files:  tc.files,
				Config: codeshape.Config{Languages: tc.config},
			}
			got := codeshape.Collect(in).LanguagesByPosture
			if !slices.Equal(got.Preferred, tc.want.preferred) {
				t.Errorf("Preferred = %v, want %v", got.Preferred, tc.want.preferred)
			}
			if !slices.Equal(got.Allowed, tc.want.allowed) {
				t.Errorf("Allowed = %v, want %v", got.Allowed, tc.want.allowed)
			}
			if !slices.Equal(got.Anomalous, tc.want.anomalous) {
				t.Errorf("Anomalous = %v, want %v", got.Anomalous, tc.want.anomalous)
			}
		})
	}
}

func TestCollect_ExceedsMaxLOC(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		additions     int
		deletions     int
		maxLOC        int
		wantExceeds   bool
		wantThreshold int
	}{
		{"maxLOC unset means no opinion", 9999, 9999, 0, false, 0},
		{"under threshold", 500, 200, 1000, false, 1000},
		{"exactly at threshold is not exceeded (strict greater-than)", 600, 400, 1000, false, 1000},
		{"one over threshold", 600, 401, 1000, true, 1000},
		{"far over threshold", 8000, 200, 1000, true, 1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := codeshape.Input{
				Additions: tc.additions,
				Deletions: tc.deletions,
				Config:    codeshape.Config{MaxLOC: tc.maxLOC},
			}
			sig := codeshape.Collect(in)
			if sig.ExceedsMaxLOC != tc.wantExceeds {
				t.Errorf("ExceedsMaxLOC = %v, want %v", sig.ExceedsMaxLOC, tc.wantExceeds)
			}
			if sig.MaxLOCThreshold != tc.wantThreshold {
				t.Errorf("MaxLOCThreshold = %d, want %d", sig.MaxLOCThreshold, tc.wantThreshold)
			}
		})
	}
}

func TestCollect_Languages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []codeshape.File
		want  []string
	}{
		{"no files", nil, nil},
		{"single Go file", []codeshape.File{{Path: "main.go"}}, []string{"Go"}},
		{"two files same language deduped", []codeshape.File{
			{Path: "pkg/a.go"},
			{Path: "pkg/b.go"},
		}, []string{"Go"}},
		{"multiple languages sorted", []codeshape.File{
			{Path: "main.go"},
			{Path: "ui/app.tsx"},
			{Path: "scripts/build.py"},
		}, []string{"Go", "Python", "TypeScript"}},
		{"JavaScript variants merge", []codeshape.File{
			{Path: "a.js"},
			{Path: "b.jsx"},
			{Path: "c.mjs"},
			{Path: "d.cjs"},
		}, []string{"JavaScript"}},
		{"TypeScript variants merge", []codeshape.File{
			{Path: "a.ts"},
			{Path: "b.tsx"},
		}, []string{"TypeScript"}},
		{"C and C++ are distinct", []codeshape.File{
			{Path: "a.c"},
			{Path: "b.cpp"},
		}, []string{"C", "C++"}},
		{"C# .cs", []codeshape.File{{Path: "Program.cs"}}, []string{"C#"}},
		{"Dockerfile basename", []codeshape.File{{Path: "Dockerfile"}}, []string{"Dockerfile"}},
		{"Dockerfile in subdir", []codeshape.File{{Path: "docker/Dockerfile"}}, []string{"Dockerfile"}},
		{"Makefile basename", []codeshape.File{{Path: "Makefile"}}, []string{"Makefile"}},
		{"Shell .sh", []codeshape.File{{Path: "scripts/run.sh"}}, []string{"Shell"}},
		{"Lua .lua", []codeshape.File{{Path: "kong/plugins/foo/handler.lua"}}, []string{"Lua"}},
		{"Markdown .md", []codeshape.File{{Path: "README.md"}}, []string{"Markdown"}},
		{"YAML .yaml and .yml merge", []codeshape.File{
			{Path: ".github/workflows/ci.yml"},
			{Path: "k8s/deploy.yaml"},
		}, []string{"YAML"}},
		{"unknown extension ignored", []codeshape.File{{Path: "data.bin"}}, nil},
		{"no extension and no basename match", []codeshape.File{{Path: "LICENSE"}}, nil},
		{"uppercase extension still matches", []codeshape.File{{Path: "Main.GO"}}, []string{"Go"}},
		{"output is alphabetical", []codeshape.File{
			{Path: "z.rs"},
			{Path: "a.go"},
		}, []string{"Go", "Rust"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := codeshape.Collect(codeshape.Input{Files: tc.files}).Languages
			if !slices.Equal(got, tc.want) {
				t.Errorf("Collect(files=%+v).Languages = %v, want %v", tc.files, got, tc.want)
			}
		})
	}
}
