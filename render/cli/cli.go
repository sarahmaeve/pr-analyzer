// Package cli renders an analyzer.Analysis as the bar + bullets format
// described in design/PROTO.md. The rendering is a pure function over the
// Analysis — no I/O, no globals, no terminal escape codes.
package cli

import (
	"fmt"
	"strings"

	"github.com/sarahmaeve/pr-analyzer/analyzer"
)

const (
	defaultScale  = 100
	maxScale      = 1000
	maxBarGlyphs  = 30
	scaleStep     = 100
	autoScaleSlop = 28 // (adds + deletes) / 28 absorbs the up-to-2-glyph ceil() slack
)

func Render(a analyzer.Analysis) string {
	var b strings.Builder

	fmt.Fprintf(&b, "PR #%d %s %s\n", a.PR.Ref.Number, a.PR.Author, a.PR.URL)

	bar, scale, omitted := computeBar(a.CodeShape.LOC.Additions, a.CodeShape.LOC.Deletions)
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

	return b.String()
}

func computeBar(adds, deletes int) (text string, scale int, omitted bool) {
	scale = defaultScale
	if glyphWidth(adds, deletes, scale) > maxBarGlyphs {
		scale = autoScale(adds + deletes)
		if glyphWidth(adds, deletes, scale) > maxBarGlyphs {
			return "", scale, true
		}
	}
	return "[" + barGlyphs(adds, deletes, scale) + "]", scale, false
}

func glyphWidth(adds, deletes, scale int) int {
	return ceilDiv(adds, scale) + ceilDiv(deletes, scale)
}

func ceilDiv(numerator, divisor int) int {
	if numerator <= 0 {
		return 0
	}
	return (numerator + divisor - 1) / divisor
}

func autoScale(total int) int {
	target := ceilDiv(total, autoScaleSlop)
	scale := ((target + scaleStep - 1) / scaleStep) * scaleStep
	if scale < defaultScale {
		return defaultScale
	}
	if scale > maxScale {
		return maxScale
	}
	return scale
}

func barGlyphs(adds, deletes, scale int) string {
	return strings.Repeat("+", ceilDiv(adds, scale)) +
		strings.Repeat("-", ceilDiv(deletes, scale))
}
