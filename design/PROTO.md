# First-Slice Prototype

## Goal

Prove the full pipeline end-to-end on the smallest signal that is still useful: fetch a PR's metadata and file list from GitHub, derive Code Shape signals from them, and render the result as the `[+++++-----]` bar + bullets shown in `pr-analyzer.md`.

No diff content, no engineer profile, no org rules — those land in later slices once the seams in this one are confirmed.

## In scope

- A GitHub connector that reads PR metadata and the PR file list (no patch / diff content).
- A Code Shape collector that derives signals from those inputs.
- A library API (root package) that exposes the orchestrator, the result types, and the connector interface.
- A CLI renderer that emits the bar + bullets.
- A `cmd/pr-analyzer` binary that wires them together.

## Out of scope (deferred to later slices)

- Engineer Profile collector (tenure, OWNERS, prior PRs, karma list).
- Org Ruleset collector (mandatory / banned langs, max-LOC).
- Diff-content analysis (e.g. distinguishing a newly-added dependency from a version bump, AST checks).
- Real test-coverage measurement.
- PR body text parsing.
- Connectors beyond GitHub.
- Renderers beyond CLI.
- File-based configuration (YAML / TOML). Library accepts Go options; CLI accepts flags.
- Risky-path detection. Deferred until org configuration lands; we deliberately do not introduce glob lists or regexes in this slice.
- Language preferences / aberrations. Languages are surfaced as a neutral fact in this slice; "preferred" or "banned" judgements wait on org configuration.

## Inputs

Two GitHub REST endpoints:

- `GET /repos/{owner}/{repo}/pulls/{number}` — read: `number`, `title`, `html_url`, `user.login`, `state`, `draft`, `base.ref`, `head.ref`, `additions`, `deletions`, `changed_files`, `labels[].name`, `created_at`, `updated_at`.
- `GET /repos/{owner}/{repo}/pulls/{number}/files` — read per file: `filename`, `status`, `additions`, `deletions`, `changes`.

The connector returns a normalized `PR` struct with these fields; nothing else in the slice reaches the network.

Authentication: the library accepts a caller-supplied `*http.Client` (no env reads inside library code). The CLI reads `GITHUB_TOKEN` from the environment and constructs the client itself.

## Signals produced

Each collector returns its own typed result struct. For this slice that is `codeshape.Signals`, surfaced on `Analysis.CodeShape`.

| Signal                       | Source                       | Heuristic                                                                                                                                                                            |
|------------------------------|------------------------------|--------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| `LOC` (adds, deletes, total) | PR `additions` / `deletions` | Direct read.                                                                                                                                                                         |
| `TestsTouched` (bool)        | file list                    | Path matches: `_test.go`; basenames `test_*.py`, `*_test.py`, `*_test.rb`, `*_spec.rb`; basenames containing `.test.` or `.spec.`; or any path under `tests/`, `test/`, `spec/`.       |
| `ManifestsTouched` (paths)   | file list                    | Any path is a known manifest: `go.mod`, `go.sum`, `package.json`, `package-lock.json`, `pnpm-lock.yaml`, `yarn.lock`, `requirements.txt`, `Pipfile.lock`, `Cargo.toml`, `Cargo.lock`, `Gemfile`, `Gemfile.lock`. |
| `Languages` (names)          | file list                    | Map each file's extension or basename to a language name; deduplicate and sort. Initial seed: Go, JavaScript, TypeScript, Python, Rust, Ruby, Java, Kotlin, Swift, C, C++, C#, Shell, Markdown, YAML, JSON, HTML, CSS, Dockerfile, Makefile. Surfaced as a neutral fact — no preference / aberration judgement until org config lands. |

There is no separate `Shape` classification (`MostlyAdditions` / `MostlyDeletions` / `Mixed`): the bar already visualizes the adds-vs-deletes ratio, and any threshold would be arbitrary. Embedders that want a categorical label can derive it from `LOC.Additions` and `LOC.Deletions`.

The renderer reads `Analysis.CodeShape` directly; it does not interpret a generic signal bag.

## Module layout

```
pr-analyzer/
├── analyzer/             # package analyzer — Analysis, PR, PRRef, PRSource, Analyze()
│   └── analyzer.go
├── codeshape/            # package codeshape — the Code Shape collector + Signals struct
├── connectors/
│   └── github/           # package github — PRSource implementation against the GitHub REST API
├── render/
│   └── cli/              # package cli — bar + bullets renderer
├── cmd/
│   └── pr-analyzer/      # main — CLI binary, wires the pieces
├── design/
│   ├── pr-analyzer.md
│   └── PROTO.md
└── go.mod
```

The library lives at `<module>/analyzer`, not at the module root. Reason: Go normalizes hyphens out of package names, so a root package under `github.com/sarahmaeve/pr-analyzer` would be `package pranalyzer` and produce ugly `pranalyzer.PR` call sites. The `analyzer/` subdirectory pattern (used by `google/go-github`) keeps call sites clean as `analyzer.PR`.

Embedders import `<module>/analyzer` for the orchestrator and `<module>/codeshape` for the collector; they choose their own renderer and connector or ship a custom one. `cmd/pr-analyzer` is the only place that pulls in all four pieces together.

`internal/` is deliberately unused — anything an embedder might reasonably want lives in an importable package.

## Public API sketch

In `analyzer/`:

```go
type PRRef struct { Owner, Repo string; Number int }

type PR struct {
    Ref          PRRef
    Title        string
    Author       string
    URL          string
    State        string
    Draft        bool
    BaseRef      string
    HeadRef      string
    Additions    int
    Deletions    int
    ChangedFiles int
    Labels       []string
    Files        []PRFile
    CreatedAt    time.Time
    UpdatedAt    time.Time
}

type PRFile struct {
    Path      string
    Status    string // added | modified | removed | renamed
    Additions int
    Deletions int
}

type PRSource interface {
    FetchPR(ctx context.Context, ref PRRef) (PR, error)
}

type Analysis struct {
    PR        PR
    CodeShape codeshape.Signals
}

func Analyze(ctx context.Context, src PRSource, ref PRRef) (Analysis, error)
```

The author identifier in `PR.Author` is whatever the connector chose to put there. The GitHub connector fills it with `user.login` (e.g. `sarahmaeve`); a future corp-aware connector may resolve a display name or email instead.

In `codeshape/` (leaf — imports nothing else from pr-analyzer):

```go
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

func Collect(in Input) Signals
```

`codeshape` deliberately defines its own `Input` and `File` types rather than importing `analyzer.PR` / `analyzer.PRFile`. This breaks the import cycle (`analyzer.Analysis` already references `codeshape.Signals`) and keeps `codeshape` usable standalone — an embedder feeding a non-GitHub source can construct an `Input` directly. The orchestrator translates `PR → codeshape.Input` before calling `Collect`.

No configuration options exist on `Analyze` in this slice — none are needed yet. When the first option arrives (e.g. for risky-path globs once org config lands), the signature gains `opts ...Option`, which is non-breaking for callers passing no options.

## Bar rendering

- **Glyphs.** `+` for additions, `-` for deletions. No fill, no padding — bar width is content-determined.
- **Per-side count.** `ceil(side / scale)`. A side with at least one LOC always produces at least one glyph; a side with zero LOC produces none.
- **Default scale.** 100 LOC per glyph.
- **Auto-scale.** If at scale = 100 the bar's width would exceed 30 characters (`adds_glyphs + deletes_glyphs > 30`), recompute scale in one closed-form step:

  ```
  scale = max(100, min(1000, ceilToNearest100((adds + deletes) / 28)))
  ```

  The `/28` (rather than `/30`) absorbs the up-to-2-glyph slack from the two `ceil()` calls, so this single computation is guaranteed to yield a valid scale without an iterate-and-verify step.

- **Above the cap.** If at `scale = 1000` the bar would *still* exceed 30 characters (a 50k-LOC-and-up PR), the renderer omits the bar entirely. The numeric bullet (`adds: N  deletes: N  files: N`) is sufficient — a multi-line bar for an unmanageable PR is just noise.

- **Scale display.** When `scale != 100`, the renderer appends `  scale: <N> LOC/glyph` to the bar line. At default scale, no notice.

## Renderer output (worked examples)

**Small PR — default scale.** 320 adds, 180 deletes, 7 files, tests touched, no dep manifest, language: Go.
At scale = 100: `4 + 2 = 6 glyphs ≤ 30`. Stays at default.

```
PR #113 sarahmaeve https://github.com/example/repo/pull/113
[++++--]
adds: 320  deletes: 180  files: 7
tests touched
no dependency manifest touched
languages: Go
```

**Medium PR — auto-scaled.** 2,500 adds, 1,500 deletes.
At scale = 100 the bar would be `25 + 15 = 40 glyphs`. Apply formula: `4000 / 28 ≈ 143`, rounded up to **200**. At scale = 200: `13 + 8 = 21 ≤ 30`. ✓

```
PR #420 sarahmaeve https://github.com/example/repo/pull/420
[+++++++++++++--------]  scale: 200 LOC/glyph
adds: 2500  deletes: 1500  files: 47
tests touched
dependency manifest touched: package.json, pnpm-lock.yaml
languages: JavaScript, TypeScript
```

**Huge PR — bar omitted.** 30,000 adds, 20,000 deletes. At scale = 1000: `30 + 20 = 50 > 30`, so the bar is dropped; numbers stand on their own.

```
PR #999 sarahmaeve https://github.com/example/repo/pull/999
adds: 30000  deletes: 20000  files: 312
tests touched
dependency manifest touched: package.json, package-lock.json
languages: JavaScript, JSON, TypeScript
```

(Final wording of bullet lines is a renderer concern — they mirror the design doc's bullet style but stay Code-Shape-only for this slice.)

## Test plan (Red / Green TDD per CLAUDE.md)

For each piece below: write the failing test first, then the minimum code that turns it green. Use real values where possible and avoid mocks that exist only to dodge TDD.

Order:

1. **`codeshape` collector** — pure function `Collect(in codeshape.Input) Signals`. Drive every signal (`LOC`, `TestsTouched`, `ManifestsTouched`, `Languages`) with table-driven tests over Go struct fixtures.
2. **CLI renderer** — pure function `Render(a Analysis) string`. Table-driven, covering: default-scale bar, auto-scaled bar with scale notice, bar-omitted huge PR, and zero-LOC PR.
3. **Orchestrator** `analyzer.Analyze` — uses a fake in-memory `PRSource`; verifies wiring and assembly, not GitHub behavior.
4. **GitHub connector** — `httptest.NewServer` returns canned JSON fixtures captured from real GitHub responses (stored in `connectors/github/testdata/`). Tests verify field mapping only; no live network in any test.
5. **`cmd/pr-analyzer`** — one smoke test that runs the compiled binary against the `httptest` server (with `GITHUB_TOKEN` read from the process env) and asserts the rendered output.

Fixtures live in `testdata/` directories next to each package.

## Non-goals reminder

We are not building a Herald-style rules engine, a notification system, or a policy enforcer. pr-analyzer emits a signal; downstream tools (or humans) decide what to do with it.

## Status

The slice is implemented end-to-end: `cmd/pr-analyzer` accepts both `owner/repo#number` and full GitHub PR URLs, fetches via `connectors/github`, runs `analyzer.Analyze`, and prints `render/cli`'s bar + bullets. Verified live against `sarahmaeve/signatory#144`.
