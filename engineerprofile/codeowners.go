package engineerprofile

import (
	"bufio"
	"bytes"
	pathpkg "path"
	"slices"
	"strings"
)

// CodeownersResult reports a PR author's ownership relative to a set of
// changed paths, derived from a CODEOWNERS file's bytes. It is the first
// clone-derived Engineer Profile signal: pure (no I/O, no git), so the
// caller supplies the file content and pr-analyzer stays git-free.
//
// Matching follows GitHub's CODEOWNERS rules (gitignore-style patterns,
// last matching line wins). Known limitations, by design: only DIRECT
// @login ownership is detected — team ownership (@org/team) and email
// owners are not resolved to a user; and the glob subset covers the
// common cases (catch-all *, rooted/dir patterns, extension globs, exact
// paths) but not full gitignore semantics (e.g. ** across directories).
type CodeownersResult struct {
	Present          bool     `json:"present"`
	IsCodeowner      bool     `json:"is_codeowner"`       // login is listed as an owner anywhere
	OwnsChangedPaths bool     `json:"owns_changed_paths"` // login owns >= 1 changed path
	OwnedPaths       []string `json:"owned_paths,omitempty"`
}

type codeownersRule struct {
	pattern string
	owners  []string // lowercased owner tokens (e.g. "@alice")
}

// ParseCodeowners computes a CodeownersResult for authorLogin against
// changedPaths. content is a CODEOWNERS file's bytes (empty/nil → a zero
// result with Present=false). Pure and deterministic.
func ParseCodeowners(content []byte, authorLogin string, changedPaths []string) CodeownersResult {
	rules := parseCodeownersRules(content)
	res := CodeownersResult{Present: len(rules) > 0}
	if !res.Present {
		return res
	}

	handle := "@" + strings.ToLower(authorLogin)
	for _, r := range rules {
		if slices.Contains(r.owners, handle) {
			res.IsCodeowner = true
			break
		}
	}
	for _, p := range changedPaths {
		if owners := ownersForPath(rules, p); slices.Contains(owners, handle) {
			res.OwnedPaths = append(res.OwnedPaths, p)
		}
	}
	res.OwnsChangedPaths = len(res.OwnedPaths) > 0
	return res
}

func parseCodeownersRules(content []byte) []codeownersRule {
	var rules []codeownersRule
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue // a pattern with no owners "unsets" ownership in git; irrelevant here
		}
		owners := make([]string, 0, len(fields)-1)
		for _, o := range fields[1:] {
			owners = append(owners, strings.ToLower(o))
		}
		rules = append(rules, codeownersRule{pattern: fields[0], owners: owners})
	}
	return rules
}

// ownersForPath returns the owners of the LAST rule matching path
// (CODEOWNERS is last-match-wins), or nil if no rule matches.
func ownersForPath(rules []codeownersRule, path string) []string {
	var owners []string
	for _, r := range rules {
		if codeownersMatch(r.pattern, path) {
			owners = r.owners
		}
	}
	return owners
}

// codeownersMatch reports whether a CODEOWNERS pattern matches a repo-
// relative posix path. Covers the common GitHub forms; see the
// CodeownersResult doc for the documented limitations.
func codeownersMatch(pattern, path string) bool {
	if pattern == "*" {
		return true
	}
	pat := strings.TrimPrefix(pattern, "/")
	switch {
	case strings.HasSuffix(pat, "/"):
		// Directory pattern: the dir and everything beneath it.
		return strings.HasPrefix(path, pat)
	case strings.Contains(pat, "*"):
		if ok, _ := pathpkg.Match(pat, path); ok {
			return true
		}
		// An unanchored glob (no slash, e.g. "*.md") matches by basename
		// anywhere in the tree.
		if !strings.Contains(pattern, "/") {
			if ok, _ := pathpkg.Match(pat, pathpkg.Base(path)); ok {
				return true
			}
		}
		return false
	default:
		// Exact file, or a directory prefix.
		return path == pat || strings.HasPrefix(path, pat+"/")
	}
}
