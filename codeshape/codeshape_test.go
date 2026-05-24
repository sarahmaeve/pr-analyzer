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
