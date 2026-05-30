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
	Additions int `json:"additions"`
	Deletions int `json:"deletions"`
	Total     int `json:"total"`
}

type Signals struct {
	LOC               LOC      `json:"loc"`
	TestsTouched      bool     `json:"tests_touched"`
	ManifestsTouched  []string `json:"manifests_touched"`
	Languages         []string `json:"languages"`
	RiskyPathsTouched []string `json:"risky_paths_touched"`
	// AgentConfigPathsTouched lists PR file paths matching the
	// AI-agent configuration catalog (.cursorrules, CLAUDE.md, .claude/,
	// .cursor/, etc.). Built-in, not org-customized: the catalog names
	// a cross-codebase threat class (prompt-injection vector via agent
	// config files), not a per-org preference.
	AgentConfigPathsTouched []string `json:"agent_config_paths_touched"`
	// ExceedsMaxLOC is true iff Config.MaxLOC was set (>0) and
	// LOC.Total exceeds it (strict greater-than).
	ExceedsMaxLOC bool `json:"exceeds_max_loc"`
	// MaxLOCThreshold echoes Config.MaxLOC so the renderer can report it
	// alongside the bullet. Zero when no opinion is configured.
	MaxLOCThreshold int `json:"max_loc_threshold"`
	// LanguagesByPosture buckets detected programming languages by the
	// project's posture. Non-programming languages (Markdown, YAML, etc.)
	// never appear here. Zero-value when no posture lists are configured.
	LanguagesByPosture LanguagesByPosture `json:"languages_by_posture"`
}

// LanguagesByPosture buckets detected languages by the project's
// posture from Config.Languages.
type LanguagesByPosture struct {
	// Preferred contains languages present in Config.Languages.Preferred.
	Preferred []string `json:"preferred"`
	// Allowed contains languages in Config.Languages.Allowed that are NOT
	// also in Preferred.
	Allowed []string `json:"allowed"`
	// Anomalous contains detected programming languages absent from both
	// Preferred and Allowed.
	Anomalous []string `json:"anomalous"`
}

func Collect(in Input) Signals {
	loc := LOC{
		Additions: in.Additions,
		Deletions: in.Deletions,
		Total:     in.Additions + in.Deletions,
	}
	exceeds, threshold := deriveMaxLOC(loc.Total, in.Config.MaxLOC)
	languages := DetectLanguages(in.Files)
	return Signals{
		LOC:                     loc,
		TestsTouched:            anyTestFile(in.Files),
		ManifestsTouched:        TouchedManifests(in.Files),
		Languages:               languages,
		RiskyPathsTouched:       touchedRiskyPaths(in.Files, in.Config.RiskyPaths),
		AgentConfigPathsTouched: touchedAgentConfig(in.Files),
		ExceedsMaxLOC:           exceeds,
		MaxLOCThreshold:         threshold,
		LanguagesByPosture:      BucketLanguages(languages, in.Config.Languages),
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
	"Lua":        {},
}

func isProgrammingLanguage(name string) bool {
	_, ok := programmingLanguages[name]
	return ok
}

// BucketLanguages partitions detected languages by the project's posture:
// Preferred / Allowed / Anomalous, where Anomalous is a programming
// language (per isProgrammingLanguage — markup like Markdown/YAML/JSON is
// excluded) present in neither list. A zero-value LanguageConfig (no
// preferred and no allowed) returns a zero-value LanguagesByPosture: the
// project has expressed no opinion. Exported so other tools consuming a
// shared pr-analyzer.yaml (e.g. signatory's pr-scan) apply the same
// acceptable/not-acceptable weighting without reimplementing it.
func BucketLanguages(detected []string, cfg LanguageConfig) LanguagesByPosture {
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

// MatchesRiskyPath reports whether a single repo-relative path is covered
// by one of the configured risky-path prefixes. Match semantics: a
// pattern P matches a path F iff P == F or P + "/" is a prefix of F (no
// wildcards). Matching is byte-exact and case-sensitive and applies no
// path normalization, so external callers must pass cleaned, slash-
// separated repo-relative paths — the form pr-analyzer's own connectors
// already emit. Exported so other tools consuming a shared
// pr-analyzer.yaml (e.g. signatory's pr-scan) apply the same org policy
// to a changelist without reimplementing — and possibly diverging from —
// the rule.
func MatchesRiskyPath(filePath string, patterns []string) bool {
	for _, p := range patterns {
		if p == "" {
			continue
		}
		if filePath == p || strings.HasPrefix(filePath, p+"/") {
			return true
		}
	}
	return false
}

// touchedRiskyPaths returns the PR file paths that match one of the
// configured prefixes (see MatchesRiskyPath). A file matching multiple
// patterns is reported once. Output preserves file-list order.
func touchedRiskyPaths(files []File, patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	var out []string
	for _, f := range files {
		if MatchesRiskyPath(f.Path, patterns) {
			out = append(out, f.Path)
		}
	}
	return out
}

// agentConfigFilenames catalogs the basenames whose presence in a PR
// signals an AI-agent configuration touch. Match is case-sensitive: the
// threat-derived corpus uses these exact spellings, and a case-insensitive
// match would balloon the false-positive surface (e.g. `claude.md` as a
// notes file). Catalog is built-in — these are a cross-codebase threat
// class, not a per-org preference. See
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

// TouchedManifests returns the PR file paths whose basename is a known
// dependency manifest or lockfile (go.mod, package.json, Cargo.toml/lock,
// requirements.txt, Gemfile/lock, …). Output preserves file-list order.
// Exported so other tools (signatory's pr-scan) flag dependency-manifest
// changes from the same catalog rather than a divergent one.
func TouchedManifests(files []File) []string {
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
	".lua":      "Lua",
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

// DetectLanguages returns the sorted, unique set of languages present in
// files, classified by basename then extension. Path-only (no content),
// so a caller with just a changelist can use it. Exported alongside
// BucketLanguages so importers detect languages by pr-analyzer's mapping
// rather than a divergent one.
func DetectLanguages(files []File) []string {
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
