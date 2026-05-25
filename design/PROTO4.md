# Fourth-Slice Prototype: Engineer Profile (foundation pass)

## Goal

Introduce a new collector dimension — **Engineer Profile** — that
surfaces contributor-trust signals about a PR. Slice 4 ships the
package, the wiring, and the first signal end-to-end. The signal is
metadata-only: it is derived from data the GitHub connector already
fetches in the existing PR-detail call. **No new API calls.**

Heavier signals (OWNERS / CODEOWNERS membership, `git log`-derived
prior-contribution counts, cross-repo tenure) require the local
clone directory plumbed in slice 3 and land in a later slice.

## In scope

- New leaf package `engineerprofile/` mirroring `codeshape/`'s
  shape: `Input` + `Signals` + `Collect`. Pure, no upward deps,
  importable standalone.
- First signal: `AuthorAssociation string` — opaque,
  connector-defined string. For GitHub it carries the value of the
  PR's `author_association` field (`OWNER`, `MEMBER`,
  `COLLABORATOR`, `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`,
  `FIRST_TIMER`, `MANNEQUIN`, `NONE`).
- Connector plumbing: `analyzer.PR.AuthorAssociation string` field
  populated by the GitHub connector from the `author_association`
  JSON key. Field is a string so future non-GitHub connectors
  (GitLab, Bitbucket, corporate Gerrit, etc.) can emit whatever
  trust-bucket vocabulary their platform uses.
- `analyzer.Analysis.EngineerProfile engineerprofile.Signals` field,
  populated by `Analyze` alongside `CodeShape`.
- Renderer bullet shown **only when the value is "interesting"** —
  see "Interestingness rule" below.

## Out of scope (deferred)

- OWNERS / CODEOWNERS file parsing — the actual `LocalCloneDir`
  consumer. Slice 5+.
- `git log`-derived prior-commit signals (has this author
  committed to this repo before? when was their first commit?).
  Requires local clone, not API. Slice 5+.
- Cross-repo / account-tenure signals (account age, prior
  contributions to other repos). Out of scope unless and until we
  decide pr-analyzer should ever make additional API calls.
- Per-project Engineer Profile config (e.g. trust thresholds,
  required reviewers, OWNERS path override). Slice 5+.
- Author identity beyond `analyzer.PR.Author` (the connector-defined
  identifier string that already exists).

## API budget rule (load-bearing)

This is a hard constraint informing the slice's shape: **pr-analyzer
makes as few GitHub API calls as possible.** Today the GitHub
connector makes one PR-detail call plus one paginated file-list
call. That is the budget; Engineer Profile slice 4 does not grow
it. The `author_association` value comes out of the PR-detail
response we already fetch.

Heavier engineer-profile work (contribution history, account
tenure) will not be done via the API — it goes through the local
clone (`git log`, OWNERS files, etc.) per slice 3's plumbing.

## Interestingness rule

The renderer hides the bullet for values that carry no signal —
clearly-trusted associations from established maintainers — and
shows it for everything else. The split:

| Hidden (uninteresting)   | Shown (interesting)       |
|--------------------------|---------------------------|
| `OWNER`                  | `CONTRIBUTOR`             |
| `MEMBER`                 | `FIRST_TIME_CONTRIBUTOR`  |
| `COLLABORATOR`           | `FIRST_TIMER`             |
|                          | `MANNEQUIN`               |
|                          | `NONE`                    |
|                          | _any unknown future value_|

The hidden set is a small, named allowlist of "this person has
write or merge access to this repo" associations. Anything else
surfaces — including unknown values that GitHub may add later
(if a new enum appears, something noteworthy has changed and the
reader should see it).

This is a **rendering** rule, not a collection rule. The collector
always emits the raw string; the renderer applies the filter.
Library consumers get the full value and can apply their own
classification.

## Schema

No YAML changes in slice 4. Engineer Profile gains its own block
only when a future slice introduces engineer-profile knobs.

## Library API

```go
type Analysis struct {
    PR              PR
    CodeShape       codeshape.Signals
    EngineerProfile engineerprofile.Signals
    Config          Config
}
```

```go
package engineerprofile

type Input struct {
    AuthorAssociation string
}

type Signals struct {
    AuthorAssociation string
}

func Collect(in Input) Signals { ... }
```

`Collect` in slice 4 is a pass-through that echoes the input into
the signals struct. The package shape (`Input` / `Signals` /
`Collect`) is the load-bearing piece — it establishes the
established collector pattern so slice 5's OWNERS work has somewhere
obvious to land.

## Renderer

A single new bullet, rendered immediately after the agent-config
bullet (the natural neighbor: both are "who/what looks unusual
about this PR"):

```
author association: FIRST_TIME_CONTRIBUTOR
```

Shown only when `engineerprofile.Signals.AuthorAssociation` is in
the "interesting" set above (or is unknown). Hidden when empty or
in the trusted allowlist.

## Module impact

- `engineerprofile/engineerprofile.go` — new leaf package.
- `engineerprofile/config.go` — empty for slice 4; placeholder for a
  later slice's engineer-profile knobs. _Or_ skip this file entirely
  and add it later. **Decision: skip; add when needed.**
- `analyzer/analyzer.go` — `PR` gains `AuthorAssociation string`.
- `analyzer/analyze.go` — `Analysis` gains `EngineerProfile`; wire
  `engineerprofile.Collect` into `Analyze`.
- `connectors/github/github.go` — `prPayload` gains
  `AuthorAssociation string \`json:"author_association"\``;
  `fetchPRDetail` writes it into the returned `PR`.
- `render/cli/cli.go` — emit the bullet when the value is
  interesting.
- `cmd/pr-analyzer/main_test.go` — assert the bullet **does not**
  appear for pr_144 fixture (`OWNER`), **does** appear for trapdoor
  fixture (`FIRST_TIME_CONTRIBUTOR`).
- `connectors/github/github_test.go` — pin the parse path
  (`pr.AuthorAssociation == "OWNER"` for pr_144,
  `"FIRST_TIME_CONTRIBUTOR"` for trapdoor).
- No fixture changes — the field is already present in both
  existing fixtures.

## Test plan (TDD, mirrors slice-2/3 audits)

1. **Connector parses `author_association`.** Add assertions to the
   existing `TestClient_FetchPR_PR144Fixture` and
   `TestClient_FetchPR_TrapdoorFixture` tests: `pr.AuthorAssociation`
   matches the fixture's JSON value.
2. **engineerprofile.Collect echoes input.** Pure unit test in
   `engineerprofile/engineerprofile_test.go`.
3. **Analyze populates EngineerProfile.** Unit test in
   `analyzer/analyze_test.go` exercising a fake `PRSource` that
   returns `PR{AuthorAssociation: "FIRST_TIMER"}` and asserting
   `analysis.EngineerProfile.AuthorAssociation == "FIRST_TIMER"`.
4. **Renderer interestingness — table-driven** with the full enum:
   - `OWNER`, `MEMBER`, `COLLABORATOR`, `""` → bullet absent.
   - `CONTRIBUTOR`, `FIRST_TIME_CONTRIBUTOR`, `FIRST_TIMER`,
     `MANNEQUIN`, `NONE`, `SOMETHING_NEW_GITHUB_ADDED` → bullet
     present with the exact value.
5. **Smoke tests confirm end-to-end behavior**:
   - `TestSmoke_PR144` gains an assertion that the bullet is absent
     (OWNER).
   - `TestSmoke_TrapdoorFixture` gains an assertion that
     `"author association: FIRST_TIME_CONTRIBUTOR\n"` appears in
     the rendered output.

## Resolved decisions

- **Connector-defined string** for `AuthorAssociation` — GitHub's
  enum doesn't translate cleanly to GitLab/Bitbucket/corp
  connectors, and the field is opaque to pr-analyzer below the
  rendering layer. Matches the existing precedent of
  `analyzer.PR.Author` (also a connector-defined string).
- **Interestingness is a rendering rule, not a collection rule.**
  Collector always emits the raw value; renderer filters. Library
  consumers see everything.
- **Allowlist the trusted set, surface everything else.** A new
  GitHub enum value that we haven't catalogued surfaces by default
  — safer than silently hiding it.
- **No new API calls.** `author_association` is already in the
  PR-detail response. Heavier engineer-profile signals are clone-
  scanning work and live in a later slice.
- **No new package config file in slice 4.** Empty `config.go`
  would be speculative ceremony; add it when a knob exists.

## Non-goals reminder

Engineer Profile in slice 4 is metadata-only. The
`LocalCloneDir`-consuming work (OWNERS, git log, contribution
history) is the *next* slice. This one establishes the package and
the renderer surface so that slice has somewhere obvious to land.
