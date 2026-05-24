// Package codeshape derives Code Shape signals from a PR's metadata
// and file list. It is a leaf package: it imports nothing else from
// pr-analyzer, so an embedder may use it standalone by constructing
// its own Input.
package codeshape

import (
	"path"
	"slices"
	"strings"
)

type Input struct {
	Additions int
	Deletions int
	Files     []File
}

type File struct {
	Path string
}

type LOC struct {
	Additions int
	Deletions int
	Total     int
}

type Signals struct {
	LOC              LOC
	TestsTouched     bool
	ManifestsTouched []string
	Languages        []string
}

func Collect(in Input) Signals {
	return Signals{
		LOC: LOC{
			Additions: in.Additions,
			Deletions: in.Deletions,
			Total:     in.Additions + in.Deletions,
		},
		TestsTouched:     anyTestFile(in.Files),
		ManifestsTouched: touchedManifests(in.Files),
		Languages:        detectLanguages(in.Files),
	}
}

var manifestBasenames = map[string]struct{}{
	"go.mod":            {},
	"go.sum":            {},
	"package.json":      {},
	"package-lock.json": {},
	"pnpm-lock.yaml":    {},
	"yarn.lock":         {},
	"requirements.txt":  {},
	"Pipfile.lock":      {},
	"Cargo.toml":        {},
	"Cargo.lock":        {},
	"Gemfile":           {},
	"Gemfile.lock":      {},
}

func touchedManifests(files []File) []string {
	var out []string
	for _, f := range files {
		if _, ok := manifestBasenames[path.Base(f.Path)]; ok {
			out = append(out, f.Path)
		}
	}
	return out
}

var languageByExt = map[string]string{
	".go":       "Go",
	".js":       "JavaScript",
	".jsx":      "JavaScript",
	".mjs":      "JavaScript",
	".cjs":      "JavaScript",
	".ts":       "TypeScript",
	".tsx":      "TypeScript",
	".py":       "Python",
	".rs":       "Rust",
	".rb":       "Ruby",
	".java":     "Java",
	".kt":       "Kotlin",
	".kts":      "Kotlin",
	".swift":    "Swift",
	".c":        "C",
	".h":        "C",
	".cpp":      "C++",
	".cc":       "C++",
	".cxx":      "C++",
	".hpp":      "C++",
	".hxx":      "C++",
	".cs":       "C#",
	".sh":       "Shell",
	".bash":     "Shell",
	".md":       "Markdown",
	".markdown": "Markdown",
	".yml":      "YAML",
	".yaml":     "YAML",
	".json":     "JSON",
	".html":     "HTML",
	".htm":      "HTML",
	".css":      "CSS",
}

var languageByBasename = map[string]string{
	"Dockerfile": "Dockerfile",
	"Makefile":   "Makefile",
}

func detectLanguages(files []File) []string {
	seen := make(map[string]struct{})
	for _, f := range files {
		base := path.Base(f.Path)
		if lang, ok := languageByBasename[base]; ok {
			seen[lang] = struct{}{}
			continue
		}
		ext := strings.ToLower(path.Ext(base))
		if lang, ok := languageByExt[ext]; ok {
			seen[lang] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for lang := range seen {
		out = append(out, lang)
	}
	slices.Sort(out)
	return out
}

func anyTestFile(files []File) bool {
	for _, f := range files {
		if isTestPath(f.Path) {
			return true
		}
	}
	return false
}

func isTestPath(p string) bool {
	dir := path.Dir(p)
	if dir != "." && dir != "/" {
		for seg := range strings.SplitSeq(dir, "/") {
			switch seg {
			case "tests", "test", "spec":
				return true
			}
		}
	}
	base := path.Base(p)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return true
	case strings.HasSuffix(base, "_test.py"):
		return true
	case strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"):
		return true
	case strings.HasSuffix(base, "_test.rb"):
		return true
	case strings.HasSuffix(base, "_spec.rb"):
		return true
	case strings.Contains(base, ".test."):
		return true
	case strings.Contains(base, ".spec."):
		return true
	}
	return false
}
