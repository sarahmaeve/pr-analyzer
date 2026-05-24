// Package cli renders an analyzer.Analysis as the bar + bullets format
// described in design/PROTO.md. The rendering is a pure function over the
// Analysis — no I/O, no globals, no terminal escape codes.
package cli

import (
	"fmt"
	"math"
	"strings"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
	"github.com/sarahmaeve/pr-analyzer/render"
)

const (
	defaultScale  = 100
	maxScale      = 1000
	maxBarGlyphs  = 30
	scaleStep     = 100
	autoScaleSlop = 28 // (adds + deletes) / 28 absorbs the up-to-2-glyph ceil() slack
)

func Render(a analyzer.Analysis, c render.Config) string {
	var b strings.Builder

	fmt.Fprintf(&b, "PR #%d %s %s\n", a.PR.Ref.Number, a.PR.Author, a.PR.URL)

	bar, scale, omitted := computeBar(a.CodeShape.LOC.Additions, a.CodeShape.LOC.Deletions, c.BarScale)
	if !omitted {
		if scale == defaultScale {
			fmt.Fprintf(&b, "%s\n", bar)
		} else {
			fmt.Fprintf(&b, "%s  scale: %d LOC/glyph\n", bar, scale)
		}
	}

	fmt.Fprintf(&b, "adds: %d  deletes: %d  files: %d\n",
		a.CodeShape.LOC.Additions, a.CodeShape.LOC.Deletions, a.PR.ChangedFiles)

	if a.CodeShape.TestsTouched {
		b.WriteString("tests touched\n")
	} else {
		b.WriteString("no tests touched\n")
	}

	if len(a.CodeShape.ManifestsTouched) > 0 {
		fmt.Fprintf(&b, "dependency manifest touched: %s\n",
			strings.Join(a.CodeShape.ManifestsTouched, ", "))
	} else {
		b.WriteString("no dependency manifest touched\n")
	}

	if len(a.CodeShape.Languages) > 0 {
		fmt.Fprintf(&b, "languages: %s\n", strings.Join(a.CodeShape.Languages, ", "))
	}

	posture := a.CodeShape.LanguagesByPosture
	if len(posture.Preferred) > 0 {
		fmt.Fprintf(&b, "languages preferred: %s\n", strings.Join(posture.Preferred, ", "))
	}
	if len(posture.Allowed) > 0 {
		fmt.Fprintf(&b, "languages allowed: %s\n", strings.Join(posture.Allowed, ", "))
	}
	if len(posture.Anomalous) > 0 {
		fmt.Fprintf(&b, "languages anomalous: %s\n", strings.Join(posture.Anomalous, ", "))
	}

	if len(a.CodeShape.RiskyPathsTouched) > 0 {
		fmt.Fprintf(&b, "risky paths touched: %s\n", strings.Join(a.CodeShape.RiskyPathsTouched, ", "))
	}

	if len(a.CodeShape.AgentConfigPathsTouched) > 0 {
		fmt.Fprintf(&b, "agent-config files touched: %s\n", strings.Join(a.CodeShape.AgentConfigPathsTouched, ", "))
	}

	if isInterestingAuthorAssociation(a.EngineerProfile.AuthorAssociation) {
		fmt.Fprintf(&b, "author association: %s\n", a.EngineerProfile.AuthorAssociation)
	}

	if a.CodeShape.ExceedsMaxLOC {
		fmt.Fprintf(&b, "exceeds max LOC: %d > %d\n", a.CodeShape.LOC.Total, a.CodeShape.MaxLOCThreshold)
	}

	return b.String()
}

// trustedAuthorAssociations is the allowlist of values whose
// presence carries no signal — the contributor has commit/merge
// rights on the target repo, so the bullet would be noise. Anything
// not in this set (including the empty string, which we treat as
// "no data", and any unknown future GitHub enum value) is treated
// as interesting and surfaces. Empty string is filtered separately
// in isInterestingAuthorAssociation so a connector that simply did
// not populate the field never produces a bullet.
var trustedAuthorAssociations = map[string]struct{}{
	"OWNER":        {},
	"MEMBER":       {},
	"COLLABORATOR": {},
}

func isInterestingAuthorAssociation(s string) bool {
	if s == "" {
		return false
	}
	_, trusted := trustedAuthorAssociations[s]
	return !trusted
}

func computeBar(adds, deletes, startingScale int) (text string, scale int, omitted bool) {
	if adds < 0 {
		adds = 0
	}
	if deletes < 0 {
		deletes = 0
	}
	if startingScale <= 0 {
		startingScale = defaultScale
	}
	scale = startingScale
	if glyphWidth(adds, deletes, scale) > maxBarGlyphs {
		scale = autoScale(adds, deletes)
		if glyphWidth(adds, deletes, scale) > maxBarGlyphs {
			return "", scale, true
		}
	}
	return "[" + barGlyphs(adds, deletes, scale) + "]", scale, false
}

func glyphWidth(adds, deletes, scale int) int {
	return ceilDiv(adds, scale) + ceilDiv(deletes, scale)
}

// ceilDiv computes ceil(numerator/divisor) for non-negative numerator,
// using the overflow-safe form (numerator-1)/divisor + 1. The naive
// (numerator + divisor - 1) / divisor form overflows when numerator is
// near math.MaxInt and produces a negative result, which panics
// strings.Repeat downstream.
func ceilDiv(numerator, divisor int) int {
	if numerator <= 0 {
		return 0
	}
	return (numerator-1)/divisor + 1
}

// autoScale picks the smallest scale in [defaultScale, maxScale] (stepped by
// scaleStep) for which the bar would fit. Takes adds and deletes separately
// so it can detect adds+deletes overflow without ever computing it.
func autoScale(adds, deletes int) int {
	// Overflow guard: if adds+deletes would overflow int, the values are
	// far past anything we can render — return maxScale and let computeBar
	// observe the bar is still too wide.
	if adds > math.MaxInt-deletes {
		return maxScale
	}
	total := adds + deletes
	if total <= 0 {
		return defaultScale
	}
	target := ceilDiv(total, autoScaleSlop)
	if target >= maxScale {
		return maxScale
	}
	scale := ((target-1)/scaleStep + 1) * scaleStep
	if scale < defaultScale {
		return defaultScale
	}
	return scale
}

func barGlyphs(adds, deletes, scale int) string {
	return strings.Repeat("+", ceilDiv(adds, scale)) +
		strings.Repeat("-", ceilDiv(deletes, scale))
}
