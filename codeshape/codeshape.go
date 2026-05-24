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
	Config    Config
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
	LOC               LOC
	TestsTouched      bool
	ManifestsTouched  []string
	Languages         []string
	RiskyPathsTouched []string
	// AgentConfigPathsTouched lists PR file paths matching the
	// AI-agent configuration catalog (.cursorrules, CLAUDE.md, .claude/,
	// .cursor/, etc.). Built-in, not project-customized: the catalog
	// names a cross-project threat class (prompt-injection vector via
	// agent config files), not a per-project preference.
	AgentConfigPathsTouched []string
	// ExceedsMaxLOC is true iff Config.MaxLOC was set (>0) and
	// LOC.Total exceeds it (strict greater-than).
	ExceedsMaxLOC bool
	// MaxLOCThreshold echoes Config.MaxLOC so the renderer can report it
	// alongside the bullet. Zero when no opinion is configured.
	MaxLOCThreshold int
	// LanguagesByPosture buckets detected programming languages by the
	// project's posture. Non-programming languages (Markdown, YAML, etc.)
	// never appear here. Zero-value when no posture lists are configured.
	LanguagesByPosture LanguagesByPosture
}

// LanguagesByPosture buckets detected languages by the project's
// posture from Config.Languages.
type LanguagesByPosture struct {
	// Preferred contains languages present in Config.Languages.Preferred.
	Preferred []string
	// Allowed contains languages in Config.Languages.Allowed that are NOT
	// also in Preferred.
	Allowed []string
	// Anomalous contains detected programming languages absent from both
	// Preferred and Allowed.
	Anomalous []string
}

func Collect(in Input) Signals {
	loc := LOC{
		Additions: in.Additions,
		Deletions: in.Deletions,
		Total:     in.Additions + in.Deletions,
	}
	exceeds, threshold := deriveMaxLOC(loc.Total, in.Config.MaxLOC)
	languages := detectLanguages(in.Files)
	return Signals{
		LOC:                     loc,
		TestsTouched:            anyTestFile(in.Files),
		ManifestsTouched:        touchedManifests(in.Files),
		Languages:               languages,
		RiskyPathsTouched:       touchedRiskyPaths(in.Files, in.Config.RiskyPaths),
		AgentConfigPathsTouched: touchedAgentConfig(in.Files),
		ExceedsMaxLOC:           exceeds,
		MaxLOCThreshold:         threshold,
		LanguagesByPosture:      bucketLanguages(languages, in.Config.Languages),
	}
}

// deriveMaxLOC reports whether the PR's total LOC exceeds the
// configured threshold. An unset (zero) threshold means "no opinion"
// and never produces a signal.
func deriveMaxLOC(total, threshold int) (exceeds bool, echoed int) {
	if threshold <= 0 {
		return false, 0
	}
	return total > threshold, threshold
}

// programmingLanguages is the subset of detected languages subject to
// posture analysis. Data / markup / build-config languages (Markdown,
// YAML, JSON, HTML, CSS, Dockerfile, Makefile) are intentionally
// absent — they never produce anomaly signals regardless of project
// posture.
var programmingLanguages = map[string]struct{}{
	"Go":         {},
	"JavaScript": {},
	"TypeScript": {},
	"Python":     {},
	"Rust":       {},
	"Ruby":       {},
	"Java":       {},
	"Kotlin":     {},
	"Swift":      {},
	"C":          {},
	"C++":        {},
	"C#":         {},
	"Shell":      {},
}

func isProgrammingLanguage(name string) bool {
	_, ok := programmingLanguages[name]
	return ok
}

// bucketLanguages partitions the detected languages by the project's
// posture. Zero-value LanguageConfig (no preferred or allowed lists)
// returns a zero-value LanguagesByPosture: the project has expressed no
// opinion and the renderer should not emit posture bullets.
func bucketLanguages(detected []string, cfg LanguageConfig) LanguagesByPosture {
	if len(cfg.Preferred) == 0 && len(cfg.Allowed) == 0 {
		return LanguagesByPosture{}
	}
	preferred := stringSet(cfg.Preferred)
	allowed := stringSet(cfg.Allowed)
	var out LanguagesByPosture
	for _, lang := range detected {
		if !isProgrammingLanguage(lang) {
			continue
		}
		if _, ok := preferred[lang]; ok {
			out.Preferred = append(out.Preferred, lang)
		} else if _, ok := allowed[lang]; ok {
			out.Allowed = append(out.Allowed, lang)
		} else {
			out.Anomalous = append(out.Anomalous, lang)
		}
	}
	return out
}

func stringSet(s []string) map[string]struct{} {
	m := make(map[string]struct{}, len(s))
	for _, v := range s {
		m[v] = struct{}{}
	}
	return m
}

// touchedRiskyPaths returns the PR file paths that match one of the
// configured prefixes. Match semantics: a pattern P matches a file path
// F iff P == F or P + "/" is a prefix of F. A file matching multiple
// patterns is reported once. Output preserves file-list order.
func touchedRiskyPaths(files []File, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	var out []string
	for _, f := range files {
		for _, p := range patterns {
			if f.Path == p || strings.HasPrefix(f.Path, p+"/") {
				out = append(out, f.Path)
				break
			}
		}
	}
	return out
}

// agentConfigFilenames catalogs the basenames whose presence in a PR
// signals an AI-agent configuration touch. Match is case-sensitive: the
// threat-derived corpus uses these exact spellings, and a case-insensitive
// match would balloon the false-positive surface (e.g. `claude.md` as a
// notes file). Catalog is built-in — these are a cross-project threat
// class, not a per-project preference. See
// signatory/design/threat-landscape/2026-05-24-trapdoor-crypto-stealer.md
// for the originating Trapdoor PR-against-legit-AI-project vector;
// .github/copilot-instructions.md is documented at
// docs.github.com/en/copilot/how-tos/copilot-on-github/customize-copilot/add-custom-instructions/add-repository-instructions.
var agentConfigFilenames = map[string]struct{}{
	".cursorrules":            {},
	"CLAUDE.md":               {},
	"AGENTS.md":               {},
	"GEMINI.md":               {},
	".windsurfrules":          {},
	".aider.conf.yml":         {},
	".aider.conf.yaml":        {},
	"copilot-instructions.md": {},
}

// agentConfigDirs catalogs path segments that, when present anywhere in a
// PR file's path, signal an AI-agent configuration touch. Whole-segment
// match — `.cursor` matches `.cursor/rules` and `apps/web/.cursor/x` but
// not `.cursor.bak/rules`. A single-segment path equal to a catalog entry
// (e.g. a file `.gemini` at repo root) is also matched, since `SplitSeq`
// yields the basename as the sole segment.
var agentConfigDirs = map[string]struct{}{
	".claude":   {},
	".cursor":   {},
	".aider":    {},
	".zed":      {},
	".codex":    {},
	".continue": {},
	".windsurf": {},
	".gemini":   {},
}

// agentConfigInstructionsSuffix matches GitHub Copilot's path-scoped
// instructions files at .github/instructions/NAME.instructions.md. The
// compound .instructions.md suffix is specific enough that a basename-
// suffix match is conservative — bare instructions.md or *.instruction.md
// (singular) do not match, and the suffix alone with no prefix does not
// match either.
const agentConfigInstructionsSuffix = ".instructions.md"

func touchedAgentConfig(files []File) []string {
	var out []string
	for _, f := range files {
		if matchesAgentConfig(f.Path) {
			out = append(out, f.Path)
		}
	}
	return out
}

func matchesAgentConfig(p string) bool {
	base := path.Base(p)
	if _, ok := agentConfigFilenames[base]; ok {
		return true
	}
	if len(base) > len(agentConfigInstructionsSuffix) && strings.HasSuffix(base, agentConfigInstructionsSuffix) {
		return true
	}
	for seg := range strings.SplitSeq(p, "/") {
		if _, ok := agentConfigDirs[seg]; ok {
			return true
		}
	}
	return false
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
