# Second-Slice Prototype: per-project configuration

## Goal

Give projects a YAML file (`pr-analyzer.yaml`) that drives the existing Code Shape collector's behavior — and produces new signals — without changing pr-analyzer's "emit, don't enforce" posture. The loader treats user files as **inputs**, not as **contract**: anything unrecognized is reported and skipped, anything recognized is applied, the rest of the run continues normally. Code is the contract; YAML is one of many ways the user might describe their intent.

Slice 1 (`PROTO.md`) shipped the end-to-end pipeline with hard-coded heuristics. Slice 2 turns four of those knobs over to the project: bar scale, risky paths, max LOC, and language posture.

## In scope

- YAML loader (`gopkg.in/yaml.v3`) with **lenient decoding**: unknown keys / wrong types → warning, continue with defaults; unparseable YAML or a missing `--config` target → fatal with line / column info.
- Discovery: walk up from CWD looking for `pr-analyzer.yaml`, stopping at the first hit or at the filesystem root. `--config=path` overrides discovery entirely.
- `codeshape.Config` and `render.Config` structs mirroring the YAML schema, threaded into `codeshape.Collect` and the renderer.
- Wiring per knob (each maps to one signal change, listed in "Signals" below).
- Renderer additions: new bullets for the new signals; no change to existing bullets when the config is silent.
- CLI arg parsing rebuilt on `github.com/alecthomas/kong` (replaces the ad-hoc `os.Args` parsing in slice 1).
- Tests using the patterns the slice-1 audit set: exact-output where deterministic, property-not-tautology, error-category asserts, every-request capture for headers, race-clean.

## Out of scope (deferred)

- Engineer Profile / OWNERS / tenure / karma lists.
- Org Ruleset collector — once `codeshape.Config` proves the loader pattern, future slices add `engineerprofile.Config` / `orgruleset.Config` symmetrically.
- Repo-fetched config (pulling `pr-analyzer.yaml` from the PR's *own* repo via the GitHub API). For now the config is **the runner's**, not the PR's repo's.
- Glob support beyond simple path-segment prefixes. We can roll our own `**` matcher later if a use case demands it; not in this slice.
- Schema versioning. `version:` field absent by design.
- User-customizable dependency-manifest seed. Adding a manifest pattern is a pr-analyzer maintainer concern (a PR to the built-in seed), not a user-config concern: all new dependencies should be made visible regardless of project preference.
- Custom test-file heuristics (beyond the slice-1 seed). Worth doing once the loader exists, but separate slice.
- Hidden / dotfile config form (`.pr-analyzer.yaml`). Single visible filename only.
- Per-collector enable / disable toggles.

## Inputs

A single YAML file at one of:

1. Path supplied via `--config <path>` (highest priority). Missing file at this path is fatal.
2. `pr-analyzer.yaml` found by walking up from CWD, stopping at the filesystem root.
3. None → defaults (slice-1 behavior; the loader returns a zero-value `Config` and no warnings).

## Schema

All keys optional. Defaults match slice 1 — a project with no opinions sees the same output as slice 1.

```yaml
# Renderer / output interface concerns.
render:
  # Override the bar's default 100 LOC/glyph. Clamped to [100, 1000];
  # auto-scale rules from PROTO.md still apply on top of this.
  bar_scale: 100

# Code Shape collector tuning.
codeshape:
  # Path-segment prefixes marking high-blast-radius areas of the codebase.
  # A pattern P matches a PR file path F if P == F or P + "/" is a prefix
  # of F. No wildcards. Examples: "billing" matches billing/foo.go and
  # billing exactly, but NOT billings/foo.go.
  risky_paths:
    - billing
    - payments
    - auth/identity

  # Maximum total LOC (additions + deletions) before the renderer adds
  # an "exceeds max LOC" bullet. Unset / 0 = no opinion. Emits a signal
  # only — pr-analyzer never blocks.
  max_loc: 1000

  # Language posture. Programming languages detected outside both lists
  # are surfaced as "anomalous" — not banned, just unexpected. Empty
  # lists = no opinion (slice-1 behavior).
  languages:
    preferred: [Go]
    allowed:   [Go, TypeScript]
```

Notably absent: no `banned:` list. Anomalies are *inferred* from "what the project listed as preferred or allowed", which is a softer framing than "this language is forbidden" and avoids re-litigating the "we emit, we don't enforce" stance.

## Language classification

Anomaly detection runs only over **programming languages**. Slice-1 already detects a language per file via extension / basename; this slice adds one classification axis on top.

**Programming-language subset** (subject to posture analysis):

> Go, JavaScript, TypeScript, Python, Rust, Ruby, Java, Kotlin, Swift, C, C++, C#, Shell.

**Non-programming languages** (always neutral — never anomalies regardless of posture lists):

> Markdown, YAML, JSON, HTML, CSS, Dockerfile, Makefile.

The latter group is data / markup / build-config. A Go-only project with one Markdown file in a PR should not see a "Markdown is anomalous" bullet. Most CI files are YAML and are exempted naturally by this rule.

The seed lists above are documented here rather than configurable: the same maintainer-vs-user-knob argument that dropped `extra_manifests` applies. If a real project breaks the seed, that's a PR to extend the seed.

## Loader policy

User files are **inputs**, not the contract. The loader is tolerant of shape mismatches and reports them; the analyzer keeps running.

- **Unparseable YAML** (broken syntax): fatal. Error message includes line and column from `yaml.v3`'s error type.
- **Missing `--config` target**: fatal. The user explicitly asked for it.
- **Missing auto-discovered file**: silent. Returns a zero-value `Config`.
- **Unknown top-level key** (e.g. `codshape:`): warning with line number; key ignored.
- **Unknown nested key** (e.g. `codeshape.lanugages:`): warning with line number; key ignored.
- **Wrong type for a known key** (e.g. `max_loc: "not a number"`): warning with line number; that key uses its zero-value default; rest of the section still loads.
- **`bar_scale` outside `[100, 1000]`**: warning, clamp to the nearest endpoint, continue.

Implementation sketch: load into both a typed `Config` struct and a `yaml.Node` tree; walk the tree to find keys absent from the typed struct's field set; report each. For type mismatches, rely on `yaml.v3`'s accumulated decode errors.

**Library is I/O-free.** The loader returns `(Config, []Warning, error)`. The CLI prints warnings to stderr; embedders can route them anywhere.

## CLI surface

Rebuilt on `github.com/alecthomas/kong`. Single struct, declarative tags.

```go
type cli struct {
    Config string `help:"Path to project config file." short:"c" type:"existingfile"`
    PR     string `arg:"" help:"PR ref: owner/repo#number or full GitHub PR URL."`
}
```

`kong.Parse(&cli)` handles `--help`, `--version`, positional arg validation, and `--config`'s existing-file check before `run()` is even called. Slice 1's hand-rolled `os.Args` slicing is removed.

## Signals produced

Each new signal is opt-in via config. If the config knob is unset, no signal is emitted and the slice-1 output is unchanged.

| Signal                                | Driven by                       | Behavior                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
|---------------------------------------|---------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `RiskyPathsTouched []string`          | `codeshape.risky_paths`         | Paths in the PR matching one of the configured prefixes. Empty when no config / no match.                                                                                                                                                                                                                                                                                                                                                                                                                                                                  |
| `ExceedsMaxLOC bool` + `MaxLOC int`   | `codeshape.max_loc`             | True iff `max_loc > 0 && (Additions + Deletions) > max_loc`. Threshold travels with the signal so the renderer can report it.                                                                                                                                                                                                                                                                                                                                                                                                                              |
| `LanguagesByPosture` (3 lists)        | `codeshape.languages.{preferred,allowed}` | Programming languages in non-CI files, bucketed by posture: `Preferred` (in `preferred:`), `Allowed` (in `allowed:` but not `preferred:`), `Anomalous` (in neither). When both posture lists are empty, this signal is absent and the slice-1 `Languages` line is unchanged.                                                                                                                                                                                                                                                                              |
| `BarScale int` (renderer)             | `render.bar_scale`              | Starting scale before auto-scale rules apply. Lives on a render-side signal (or directly on a `render.Config` passed to `Render`), not on `codeshape.Signals` — the bar is a renderer concern.                                                                                                                                                                                                                                                                                                                                                              |

## Module impact

- **`codeshape/`**:
  - New `Config` struct: `RiskyPaths []string`, `MaxLOC int`, `Languages LanguageConfig`.
  - `Input` gains `Config codeshape.Config`.
  - `Signals` gains `RiskyPathsTouched []string`, `ExceedsMaxLOC bool`, `MaxLOCThreshold int`, `LanguagesByPosture` struct.
  - `Collect` reads `Input.Config` and drives the new signals.
  - One new internal classifier function: `isProgrammingLanguage(string) bool`. Backed by a package-level set seeded as listed above.
- **`analyzer/`**:
  - `Config` struct: `type Config struct { Render render.Config; CodeShape codeshape.Config }`.
  - `Analyze` gains the slot deferred in slice 1: `Analyze(ctx, src, ref, opts ...Option)` where `WithConfig(c Config)` is the first concrete `Option`.
  - Orchestrator translates `Config.CodeShape` into `codeshape.Input.Config` alongside the file-list translation.
- **New `render/` package surface** (the existing `render/cli` continues to live under it):
  - `render.Config` struct: `BarScale int`.
  - `cli.Render` gains an optional config parameter (or takes the config off `Analysis` — TBD; see open question).
- **New `internal/configfile/` package**:
  - `Load(path string) (analyzer.Config, []Warning, error)` for explicit-path loading.
  - `Discover(startDir string) (analyzer.Config, foundPath string, []Warning, error)` for walk-up discovery.
  - `Warning` carries source line + human-readable message.
- **`cmd/pr-analyzer/`**:
  - Kong-driven CLI struct replaces hand-rolled `parsePRRef` argument handling (the URL/short-form parsing itself stays — it's the *flag plumbing* that moves).
  - `run` calls `configfile.Discover` (or `Load` if `--config` given), prints warnings to stderr, then passes the loaded `Config` to `analyzer.Analyze` via `WithConfig`.

## Renderer output (worked examples)

**Project with no config file** — identical to slice 1.

**Project with risky paths + max LOC + language posture**, PR touches `billing/charge.go`, a Go file, and a Markdown file:

```
PR #113 sarahmaeve https://github.com/example/repo/pull/113
[++++--]
adds: 320  deletes: 180  files: 7
tests touched
no dependency manifest touched
languages: Go, Markdown
languages preferred: Go
risky paths touched: billing/charge.go
```

No `languages anomalous: Markdown` because Markdown is non-programming.

**Same project, oversized PR** (8,000 + 200, `max_loc: 1000`) with an unexpected Rust file:

```
PR #420 sarahmaeve https://github.com/example/repo/pull/420
[++++++++++++++++++++++++++--]  scale: 300 LOC/glyph
adds: 8000  deletes: 200  files: 47
tests touched
no dependency manifest touched
languages: Go, Rust
languages preferred: Go
languages anomalous: Rust
exceeds max LOC: 8200 > 1000
```

Bullet ordering, exact wording, and spacing are pinned in the renderer tests; examples here are illustrative.

## Test plan (TDD per piece, patterns from slice 1 audit)

Order:

1. **Schema decoding — happy path.** `configfile.Load` on a known-good YAML, exact-equality compare against a Go literal `Config`. Table-driven for each top-level section.
2. **Lenient loader — unknown keys.** YAML with `codshape:` (typo) + `codeshape.lanugages:` (typo) → load succeeds, returns the recognized fields, warnings list contains both keys with line numbers.
3. **Lenient loader — type mismatch.** `max_loc: "not a number"` → load succeeds, `max_loc` defaults to 0, warning emitted with line + field name.
4. **Lenient loader — `bar_scale` clamp.** Out-of-range value → load succeeds, clamped value applied, warning emitted.
5. **Lenient loader — unparseable YAML.** Broken syntax → fatal error with line / column info.
6. **Discovery — walk-up.** `t.TempDir()` with nested dirs, `pr-analyzer.yaml` in the parent. `Discover(cwd)` finds it, returns path. Stop condition: filesystem root.
7. **Discovery — `--config` overrides.** `Load("/explicit/path.yaml")` skips walk-up. Missing file at explicit path is fatal.
8. **`codeshape.RiskyPathsTouched`** — table-driven: `billing` matches `billing/x` and `billing` exact, not `billings/x`. Exact-list equality.
9. **`codeshape.ExceedsMaxLOC`** — threshold of 0 = no signal; 1000 with total=1001 = true; total=1000 = false (strict greater-than).
10. **`codeshape.LanguagesByPosture` — basic bucketing.** Detected `[Go, TypeScript]` with `preferred=[Go], allowed=[Go, TypeScript]` → `Preferred=[Go], Allowed=[TypeScript], Anomalous=[]`.
11. **`codeshape.LanguagesByPosture` — anomaly detection.** Detected `[Go, Rust]` with `preferred=[Go], allowed=[Go]` → `Preferred=[Go], Anomalous=[Rust]`.
12. **`codeshape.LanguagesByPosture` — non-programming exemption.** Detected `[Go, Markdown, YAML]` with `preferred=[Go]` → `Preferred=[Go]`, no anomalies (Markdown / YAML are non-programming).
13. **`codeshape.LanguagesByPosture` — empty config.** No posture lists → signal is absent / zero-value; slice-1 `Languages` line still renders.
14. **`render/cli` additions** — exact-output table: no-config matches slice-1; risky-paths bullet appears; exceeds-max bullet appears with the threshold; posture bullets appear in defined order. Renderer signature changes to `Render(a Analysis, c render.Config) string`.
15. **`analyzer.Analyze` with `WithConfig`** — fake source + explicit `Config` → all new signals flow through. Regression: `Analyze` without `WithConfig` produces slice-1-equivalent output.
16. **Kong parsing — happy path.** Parse `["--config", "x.yaml", "owner/repo#1"]` → `cli.Config == "x.yaml"`, `cli.PR == "owner/repo#1"`.
17. **Kong parsing — missing positional.** Parse `["--config", "x.yaml"]` → error from Kong before `run` is called.
18. **`cmd/pr-analyzer` smoke** — actual YAML on disk in `t.TempDir()`, `--config=/path/to/yaml`, run binary against captured PR #144 fixture, assert new bullets appear. Reuse `buildBinary` helper from slice 1.
19. **`cmd/pr-analyzer` discovery smoke** — drop `pr-analyzer.yaml` in `t.TempDir()`, set `cmd.Dir`, run binary, assert it's picked up. Verify discovery walks up if `cmd.Dir` is a subdirectory.

## Patterns to enforce (set in slice 1, applied throughout slice 2)

- **Exact-output equality** for the renderer; the test becomes the spec.
- **Property over tautology**: every assertion proves something an outside observer would care about. No counting items in a slice the test just built.
- **Error-category asserts** via `wantErrContains` for loader-error tests.
- **Every-request capture** for any test that issues multiple HTTP calls.
- **External test packages** (`_test`) where the public API allows; internal where unexported access is genuinely needed.
- **`t.Helper()`** on all test helpers. New helper candidates this slice: `writeConfig(t, dir, body)` and `loadAndWarn(t, body)`.
- **`go test -race`** baseline on every commit.

## Non-goals reminder

We emit signals; we do not enforce. `languages anomalous: Rust` produces a bullet, not an exit code. `max_loc: 1000` produces a bullet, not a refusal to render. Anomalies are softened from "banned" deliberately — pr-analyzer reports the project's posture against detected reality, never imposes.

## Resolved decisions

- **Renderer signature.** `cli.Render(a Analysis, c render.Config) string`. Analysis carries the analyzer's output; render config is supplied separately by the caller.
- **Discovery stops at filesystem root.** No git-root special case. Predictable behavior regardless of whether the user is inside a git checkout.
- **Warning order.** Returned in load order (line-ascending). The CLI prints them to stderr in that order. Mirrors how a human reads the file.
- **No CI-path detection in this slice.** The non-programming-language exemption already covers most CI files (they're typically YAML). The rare edge case of a Shell script inside `.github/workflows/` will count as Shell; if a project's posture lists don't include Shell, it surfaces as an anomaly. Acceptable.
