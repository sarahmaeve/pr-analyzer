# Fifth-Slice Prototype: Repo-wide scan + HTML / JSON renderers

## Goal

Extend pr-analyzer from "analyze one PR" to "analyze every open PR in a
repo" and emit a paired artifact pair — a self-contained HTML report
that a human can open by double-clicking, and a sibling JSON file with
the same data that downstream tools (other renderers, LLMs, CI bots,
custom dashboards) can consume.

The two artifacts are produced from the same in-memory `[]Analysis`, so
they cannot drift. The HTML embeds the JSON inline (in a
`<script type="application/json">` block) so the HTML stays
self-contained even when separated from the sibling JSON file. CSS
styling lives in a stable class / `data-*` contract so users can iterate
on look-and-feel without rebuilding the analyzer.

Single-PR mode (`owner/repo#N` → CLI text) is unchanged.

## In scope

- New CLI mode: bare `owner/repo` argument → list mode.
- New connector method on `analyzer.PRSource`:
  `ListOpenPRs(ctx, owner, repo) ([]PRRef, error)`.
- GitHub connector implements it via
  `GET /repos/{owner}/{repo}/pulls?state=open&per_page=100`, paginated.
- New `--out <dir>` flag — defaults to `.`. The CLI writes
  `<dir>/index.html` and `<dir>/analyses.json` in list mode.
- Two new renderer packages under `render/`:
  - `render/json/` — pure `Render([]analyzer.Analysis, Envelope) ([]byte, error)`.
  - `render/html/` — pure `Render([]analyzer.Analysis, ...) (string, error)`;
    internally calls `render/json` to inline the JSON.
- JSON tags on the existing analyzer / collector structs so the emitted
  JSON uses `snake_case` keys.
- Smoke test that runs the binary against a multi-PR fixture and asserts
  both artifacts on disk.

## Out of scope (deferred)

- Concurrency. PRs are fetched sequentially in slice 5; per-PR latency
  multiplies. Easy to parallelize later behind a flag once we see real
  workloads.
- Filters beyond `state=open` (label, base ref, author). The user said
  the assumption is `pulls?state=open`, full stop.
- HTML for single-PR mode. `owner/repo#N` keeps emitting CLI text. If a
  later iteration wants `pr-analyzer owner/repo#N --out=…` to also write
  HTML for one PR, it's a trivial follow-up (list-of-one).
- JavaScript in the rendered HTML. Drill-down is native `<details>` /
  `<summary>` and CSS only. Interactivity (filter, sort, search) is a
  later iteration on the same stable markup.
- A library wrapper `AnalyzeAll`. The loop is one `for range refs` in
  `cmd/pr-analyzer/main.go`; pulling it into the library is speculative
  ceremony until a second embedder needs it.
- A CSS file separate from the inline `<style>` block. The default
  styling ships inline so the HTML stays self-contained. Users who want
  to skin the report do it via the documented class / `data-*` contract
  in their own follow-up CSS, loaded however they prefer.
- Per-PR error tolerance policy decisions beyond "log and continue".
  Slice 5 logs a one-line warning to stderr for any individual
  `Analyze` failure and renders the rest; a structured error block in
  the JSON envelope can come later if needed.
- Schema versioning of the JSON envelope beyond a single
  `schema_version: 1` constant. Multi-version migration logic is a
  problem for the day we ship a v2.

## API budget impact (explicit)

This slice grows the GitHub API budget that PROTO4 established as
load-bearing. For a repo with N open PRs the slice's mode makes:

- 1 paginated call to list open PRs (`/pulls?state=open`).
- N PR-detail calls.
- ≥ N paginated PR-files calls (one page per PR; more if any PR exceeds
  100 changed files).

For Kong/kong-scale repos (dozens to ~150 open PRs) this is well inside
the authed-user 5000/hour budget. We do not add a rate-limit interceptor
in slice 5; if a later target trips the limit, we'll add backoff at the
connector level.

The cost is unavoidable for the feature — there is no metadata-only
"all signals for all PRs" endpoint to ride on. Calling this out so the
budget rule doesn't quietly erode.

## CLI surface

```
pr-analyzer Kong/kong                       # list mode → ./index.html + ./analyses.json
pr-analyzer Kong/kong --out=./report        # list mode → ./report/{index.html, analyses.json}
pr-analyzer Kong/kong#14838                 # single-PR mode (unchanged) → stdout text
pr-analyzer --config=… Kong/kong            # config still applies in list mode
```

Detection rule in `parsePRRef`: presence of `#` ⇒ single-PR mode;
absence ⇒ list mode (a new `parseRepoRef` validates `owner/repo` form
with the same character set). Full PR URLs (`/pull/N`) continue to
short-circuit to single-PR mode; a bare repo URL
(`https://github.com/owner/repo`) is **not** accepted as a list-mode
target in slice 5 — keep one input form per mode until users ask.

`--out` is created with `os.MkdirAll(dir, 0o755)` if absent. It is
ignored in single-PR mode (no silent file writes when the user didn't
ask for them).

## Connector additions

```go
// in package analyzer

type PRSource interface {
    FetchPR(ctx context.Context, ref PRRef) (PR, error)
    ListOpenPRs(ctx context.Context, owner, repo string) ([]PRRef, error)
}
```

The interface grows by one method. There is no second implementation
yet, so the BC concern is only "do not silently dispatch to a `nil`
method." Mocks in the existing tests gain a stub returning `nil, nil`.

GitHub implementation reuses the existing `getJSON` + `parseNextLink`
pagination machinery. The list endpoint returns the same shape as PR
detail, so we can reuse `prPayload` for the parse but only need
`Number` (the orchestrator re-fetches detail per PR for the full
signal set — list endpoint omits some fields that detail carries).

A response-size guard identical to the existing PR-files paginator
applies: per-page cap on body bytes, same-origin check on the next
link.

## Renderer additions

### `render/json`

Pure function:

```go
package json // package path render/json; local name "rjson" to avoid the stdlib clash

type Envelope struct {
    SchemaVersion int               `json:"schema_version"`
    GeneratedAt   time.Time         `json:"generated_at"`
    Repo          analyzer.PRRef    `json:"repo"`        // Number is zero in list mode
    Analyses      []analyzer.Analysis `json:"analyses"`
}

func Render(analyses []analyzer.Analysis, repo analyzer.PRRef, now time.Time) ([]byte, error)
```

`now` is taken as a parameter (not `time.Now()`) so tests get
deterministic output. `SchemaVersion` is a constant `1`. No
`omitempty` on collector-output fields — consumers should be able to
rely on every documented key being present.

JSON keys are `snake_case` throughout, set via `json:"…"` tags on the
existing analyzer / codeshape / engineerprofile structs. This is the
first slice to actually emit JSON, so it's the right time to pin the
tags.

### `render/html`

Pure function:

```go
package html

func Render(analyses []analyzer.Analysis, repo analyzer.PRRef, now time.Time) (string, error)
```

Builds the page with `html/template` (context-aware escaping; handles
the `</script>` footgun automatically when embedding the JSON). The
inlined JSON block is produced by calling `render/json.Render` — single
source of truth for serialization.

Page structure:

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>pr-analyzer report — owner/repo</title>
  <style>/* default inline styles */</style>
</head>
<body>
  <header class="pra-header" data-pra-repo="owner/repo" data-pra-count="N" data-pra-generated="ISO8601">
    <h1>owner/repo — N open PRs</h1>
    <p class="pra-meta">Generated 2026-05-25T10:30:00Z</p>
  </header>

  <main class="pra-prs">
    <section class="pra-pr"
             data-pra-pr-number="144"
             data-pra-pr-author="sarahmaeve"
             data-pra-pr-draft="false"
             data-pra-pr-author-association="OWNER"
             data-pra-pr-loc-additions="5631"
             data-pra-pr-loc-deletions="220"
             data-pra-pr-loc-total="5851"
             data-pra-pr-exceeds-max-loc="false"
             data-pra-pr-tests-touched="true"
             data-pra-pr-agent-config-touched="false">
      <details>
        <summary>
          <span class="pra-pr-num">#144</span>
          <span class="pra-pr-title">Add slice-4 engineer-profile foundation</span>
          <span class="pra-pr-author">sarahmaeve</span>
          <span class="pra-pr-bar" role="img" aria-label="5631 additions, 220 deletions">
            <span class="pra-pr-bar-add" style="flex-basis: 96.2%"></span>
            <span class="pra-pr-bar-del" style="flex-basis: 3.8%"></span>
          </span>
        </summary>
        <dl class="pra-pr-signals">
          <dt>LOC</dt> <dd>adds: 5631 · deletes: 220 · files: 27</dd>
          <dt>Tests</dt> <dd>touched</dd>
          <dt>Languages</dt> <dd>Go, Markdown</dd>
          <!-- ...one <dt><dd> per resolved signal... -->
        </dl>
      </details>
    </section>
    <!-- ...one <section.pra-pr> per analysis... -->
  </main>

  <script type="application/json" id="pra-data">
  { "schema_version": 1, "generated_at": "...", "repo": {...}, "analyses": [...] }
  </script>
</body>
</html>
```

**Stable markup contract** (this is what users skin against — changing
any of it is a breaking change to consumers):

- All pr-analyzer-owned classes are prefixed `pra-` (so user CSS can
  scope safely; collision with project CSS is unlikely).
- All pr-analyzer-owned data attributes are prefixed `data-pra-`.
- One `<section class="pra-pr">` per Analysis, in the order
  `ListOpenPRs` returned (GitHub's default: newest first).
- `data-pra-pr-*` attributes mirror the JSON for that PR, flattened to
  primitives. The full structured data is available via the inline JSON
  block (`#pra-data`); the attributes are the CSS-selectable
  subset.
- Bullet content lives in `<dl class="pra-pr-signals">` with one
  `<dt>`/`<dd>` pair per signal. Signal labels are stable strings.

**Default bar visualization.** The CSS bar's two segments are
proportioned **per-PR** (adds vs deletes inside that PR's total), not
normalized across the report. Per-PR keeps the green/red ratio
informative even when the report mixes a 50-line PR with a 5000-line
PR. The absolute LOC numbers are in the `<dd>` and in `data-pra-pr-loc-*`,
so cross-PR comparison stays available via either text or a future
CSS overlay that reads the attributes.

Default styling is intentionally light: card layout, monospace numbers,
green/red bar segments, no JavaScript. The user iterates from there.

## JSON tag policy

Apply `json:"snake_case"` tags to: `analyzer.PRRef`, `analyzer.PR`,
`analyzer.PRFile`, `analyzer.Analysis`, `codeshape.Signals`,
`codeshape.LOC`, `codeshape.LanguagesByPosture`,
`engineerprofile.Signals`. No tags on `Config` / `Input` structs —
they're inputs to collectors, not outputs to consumers.

`time.Time` fields serialize as RFC 3339 by default (already correct).

## Library API impact

- `analyzer.PRSource` gains `ListOpenPRs`. Embedders implementing a
  non-GitHub source now have one more method to provide.
- No new exported function on `analyzer`. The loop and renderer
  selection live in `cmd/pr-analyzer/main.go`.

## Module impact

- `analyzer/analyzer.go` — `PRSource` gains `ListOpenPRs`. Add JSON
  tags to `PR`, `PRRef`, `PRFile`, `Analysis`.
- `codeshape/codeshape.go` — JSON tags on `Signals`, `LOC`,
  `LanguagesByPosture`.
- `engineerprofile/engineerprofile.go` — JSON tags on `Signals`.
- `connectors/github/github.go` — implement `ListOpenPRs`. Add a new
  fixture and a parse-pinning test.
- `connectors/github/testdata/` — new fixtures:
  - `repo_pulls_open.json` — the list response (slice-5's listing call).
  - The existing per-PR fixtures (`pr_144.json` etc.) double as the
    detail-call responses driven from the listing.
- `render/json/json.go` — new package.
- `render/html/html.go` — new package. Inlines `render/json`'s output.
- `render/html/testdata/` — golden HTML files for renderer tests.
- `cmd/pr-analyzer/main.go` — mode detection, list-mode loop, `--out`
  flag, file writes.
- `cmd/pr-analyzer/main_test.go` — new smoke test
  `TestSmoke_RepoList` that runs the binary against a multi-PR fixture
  server and asserts both artifacts on disk plus a few stable
  substrings.

## Test plan (Red / Green TDD)

1. **`render/json.Render` happy path.** Construct a `[]Analysis` by
   hand, render, decode the bytes back, compare structurally to the
   input. Pin `schema_version: 1`, `generated_at` (from the injected
   `now`), and repo block exactness.
2. **JSON tags are snake_case.** Render an Analysis with each field
   populated, then `strings.Contains` for the expected keys
   (`"loc"`, `"tests_touched"`, `"manifests_touched"`,
   `"author_association"`, etc.). Catches a regression where someone
   adds a field without a tag.
3. **`render/html.Render` golden test.** Two-PR `[]Analysis` →
   compare to `testdata/two_prs.html` byte-for-byte. Pin the inline
   JSON block via its `<script id="pra-data">` boundaries; verify the
   `</script>` escape works (insert an analysis whose title contains
   `</script>` to confirm `html/template` neutralizes it).
4. **HTML markup contract is stable.** Independent of the golden,
   assert each documented class and data attribute appears in the
   rendered output with the expected value. This is the "skinning
   contract" test — a regression that renames `pra-pr-author` fails
   here before the golden test makes the user re-diff a giant file.
5. **GitHub `ListOpenPRs`.** httptest server returns the new
   `repo_pulls_open.json` fixture; assert the returned `[]PRRef`
   matches expected values (owner/repo/number for each).
6. **`ListOpenPRs` pagination.** Two-page fixture with `Link:
   <…>; rel="next"`; verify all refs from both pages are returned and
   the same-origin guard from `getJSON` is exercised.
7. **CLI mode detection.** Table-driven on `parsePRRef` /
   `parseRepoRef`: `owner/repo#1` → single-PR; `owner/repo` →
   list-mode; `https://github.com/owner/repo/pull/1` → single-PR;
   `https://github.com/owner/repo` → error (unsupported in slice 5).
8. **`--out` flag.** Kong parses; missing dir is created. With
   `--out=/path` and bare-repo arg, the binary writes both files; both
   parse as expected JSON / HTML.
9. **List-mode smoke** (`TestSmoke_RepoList`). httptest server serves
   `repo_pulls_open.json` (refs to PR #144 and the Trapdoor fixture)
   plus the existing per-PR fixtures. Binary runs; `--out=<tempdir>`
   produces `index.html` and `analyses.json`. Decode the JSON, count
   `analyses` length, assert both PR numbers present. In the HTML,
   assert both `<section class="pra-pr" data-pra-pr-number="144">` and
   `data-pra-pr-number="47"` are present. Assert the inline
   `<script id="pra-data">` block exists and contains valid JSON.
10. **Per-PR failure tolerance.** Inject a server that 500s on one
    PR's detail; assert the other PR still renders and stderr contains
    one warning line naming the failing PR.

## Resolved decisions

- **Inline JSON in HTML + sibling JSON file.** Self-contained artifact
  for humans (no `file://` fetch problem); machine-readable artifact
  for everything else. Both produced from the same `[]Analysis`, so
  they cannot drift.
- **Connector interface grows.** `ListOpenPRs` lives on `PRSource`
  alongside `FetchPR`. One mock to maintain, symmetric API. The
  alternative (separate `PRLister` interface) buys flexibility nothing
  in slice 5 needs.
- **Loop stays in `cmd/`.** No `analyzer.AnalyzeAll` wrapper. Add when
  a second embedder asks.
- **Sequential fetches.** Concurrency is a tuning knob for a later
  slice when a real workload demands it.
- **Default `--out` to `.`** — no required flag for the common case,
  but never write files in single-PR mode (no surprise side-effects).
- **`pra-` class prefix, `data-pra-` attribute prefix.** Stable
  contract for user-supplied CSS overlays. Short enough to be
  ergonomic, unique enough that collisions with project styling are
  unlikely.
- **Per-PR bar normalization.** The bar visualizes adds-vs-deletes
  ratio inside one PR. Cross-PR scale would either drown the small PRs
  or overflow the big ones; the numeric LOC and `data-pra-pr-loc-*`
  attributes preserve cross-PR comparability for any future overlay
  that wants to scale a different way.
- **`schema_version: 1` in the JSON envelope.** Downstream tools get a
  cheap detection point. The "no speculative ceremony" rule cuts the
  other way once an artifact is explicitly contracted to be consumed
  by other tools.
- **Per-PR errors log and continue.** A flaky PR in the middle of a
  scan does not abort the whole report. Slice 5 surfaces the warning
  to stderr; structured per-PR error blocks in the JSON envelope are a
  follow-up if someone wants to programmatically detect partial
  reports.

## Non-goals reminder

We still **emit signals, we do not enforce**. The HTML report makes
signals visible to a human reviewer; pr-analyzer does not refuse to
render a "bad" PR, color anything red as a verdict, or assign a score.
The CSS contract is intentionally neutral — `data-pra-pr-author-association="FIRST_TIME_CONTRIBUTOR"`
is a fact the user's CSS may choose to highlight, not a judgement
baked into the default output.

## Status (post-implementation)

Slice 5 shipped end-to-end. Live-dogfooded against `atuinsh/atuin`
(83 open PRs, ~3 minutes wall-clock with the 300-500ms rate limiter
applied). Both `analyses.json` and `index.html` are produced from a
single in-memory envelope, so they cannot drift.

Where the implementation matches this spec:

- New connector method `PRSource.ListOpenPRs(ctx, owner, repo)`.
- `render/json` package with `Render(analyses, repo, now)` and an
  exported `Envelope{SchemaVersion, GeneratedAt, Repo, Analyses}`
  type. JSON tags (`snake_case`) landed on every output struct.
- `render/html` package with `Render(env)` returning a self-contained
  document; CSS embedded inline (`go:embed`), JSON inlined as
  `<script type="application/json" id="pra-data">`. `html/template`'s
  JS-context auto-escaping mangled the inlined JSON, so the renderer
  splits the template before the data block and appends the raw bytes
  itself.
- CLI gained `--out` (default `.`), per-PR progress on stderr,
  log-and-continue per-PR failure tolerance, and a bare-repo argument
  form. Rate-limit transport is 300-500ms randomized, only wired in
  list mode.
- Token gate: list mode hard-requires `GITHUB_TOKEN` via the new
  `internal/credentials` package (`Vendor{Name, EnvVar}` —
  generalizes for GitLab / Bitbucket / corp Gerrit when those land).

Where it diverged from the original spec, in order:

1. **Cinematic Sci-Fi Screen Graphics design language adopted** for
   the HTML. CSS shipped with `--pra-color-*` and `--pra-space-*`
   custom properties, Orbitron / IBM Plex Mono / Exo 2 fonts via
   Google Fonts `<link>` with system-font fallbacks. Chamfered
   PR-card corners via `clip-path`; subtle radar-grid background.
2. **Bar visualization switched to cross-PR log-scaled fill.**
   Original spec called for per-PR-normalized bars; live dogfood
   showed that made it impossible to scan the column and see which
   PRs were big. Final shape: a fixed 120px lane, with a log-scaled
   fill width based on `total / maxTotalInReport`, and the per-PR
   adds-vs-deletes ratio living *inside* the fill via flex-basis.
3. **Second bar added for files-changed**, log-scaled the same way
   against the report's max files-changed. Single-segment in muted
   slate so it reads as secondary telemetry next to the primary
   blue/green LOC bar.
4. **Color routing for pills landed with semantic tiers**:
   green (`pra-pill-success`) for positive signals (CONTRIBUTOR,
   tests touched, PREFERRED LANGUAGES), orange (`pra-pill-warning`)
   for "pay attention" signals (FIRST_TIME_CONTRIBUTOR, FIRST_TIMER,
   NONE, anomalous languages, risky paths, tests not touched), red
   (`pra-pill-danger`) for severe (MANNEQUIN, agent-config touched,
   exceeds max LOC), info (`pra-pill-info`) reserved but currently
   unused.
5. **`pr-analyzer inspect <analyses.json>`** subcommand added —
   prints a deterministic single-page text summary (author-
   association distribution, languages, LOC stats with top-5,
   tests-touched count, manifests, agent-config touches). Pure
   function `summarize(env)` underneath is unit-tested for
   determinism and sort-order.
6. **`pr-analyzer render-html <analyses.json>`** subcommand added —
   reads a saved envelope and writes HTML to stdout. Lets the user
   iterate on the renderer (CSS, template) without re-fetching the
   PRs over the rate-limited API path. Together with `inspect`, this
   makes Kong's CLI structure a three-verb shape (scan / inspect /
   render-html), with scan as the default-with-args subcommand so
   the slice-1..4 invocation `pr-analyzer owner/repo[#N]` keeps
   working.

Resolved decisions that hold after dogfood:

- Inline JSON in HTML + sibling JSON file: confirmed correct. The
  HTML works opened from `file://`, the JSON is `jq`-friendly.
- `pra-` / `data-pra-` prefix: held up. Easy to skin against.
- List mode loop in `cmd/`, not in the library: still right. No
  embedder asked for `AnalyzeAll`.
- Sequential fetches: 83 PRs in 3 minutes is fine. Will revisit if
  someone scans a 500-PR repo.

Not done in slice 5 (or done differently):

- Single-PR HTML output: still deferred. `--out` is ignored in
  single-PR mode; `pr-analyzer owner/repo#N` continues to emit CLI
  text only.
- Per-PR structured error block in the JSON envelope: deferred.
  Failures still log-and-continue to stderr; analyses.json contains
  the successful subset.
- Bare repo URL (`https://github.com/owner/repo`): still rejected
  with a specific error pointing at the supported short form. No
  user has asked for it.

Next-slice prereqs landed here:

- Stable JSON tag contract on every output struct → consumable by
  downstream tooling.
- `render/json.Envelope` is exported, with `SchemaVersion` as a
  documented constant.
- `internal/credentials.Vendor` generalizes the auth-token check;
  GitLab / Bitbucket / corp Gerrit slot in with one new variable.

Live-dogfood headline numbers (atuin scan, 2026-05-25):

- 83 PRs / 0 failures / ~163KB JSON / ~510KB HTML.
- Author-association distribution: 45 NONE, 33 CONTRIBUTOR, 5 MEMBER.
- 47 PRs touch Rust; 3 PRs touch tests; 13 PRs touch Cargo manifests.
- 0 PRs touch any agent-config catalog file (the Trapdoor threat-
  class all-clear).
