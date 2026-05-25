# Sixth-Slice Prototype: Org Config Discovery (XDG / HOME)

## Goal

Let an org (an OSS project, a corporation, or any other entity with
opinions about how PRs should be analyzed) publish its
`pr-analyzer.yaml` once at a shared user-level path, instead of
dropping a copy into every repo their developers scan. The
configuration concept itself is unchanged — there is exactly one
org config, with the same schema slice 2 established. This slice
only adds two more places the loader looks for it.

## In scope

- `internal/configfile/Discover` gains two fallback sources after the
  existing CWD walk-up:
  1. `$XDG_CONFIG_HOME/pr-analyzer/pr-analyzer.yaml`
  2. `$HOME/.config/pr-analyzer/pr-analyzer.yaml`
- First-match-wins precedence. No merging across sources.
- `--config <path>` flag continues to override discovery entirely
  (slice-2 behavior).
- Misses at any discovery source remain silent; only an explicit
  `--config` pointing at a missing file is fatal.
- Tests for each new discovery source plus the precedence between
  walk-up and XDG.

## Out of scope (deferred)

- **Any form of merging or layering.** Exactly one org config is
  loaded. There is no "corp config + OSS-project config = merged
  rules" path. If a user wants the OSS project's quirks while
  scanning from a corp machine, they `cd` into the OSS checkout
  (which surfaces the project's `pr-analyzer.yaml` via walk-up)
  instead of relying on a user-level overlay.
- **`org_config:` YAML key.** No second file pointer inside the
  YAML; the file *is* the config.
- **Remote sources** (HTTP, Git refs, S3). File paths only.
- **Per-engineer overrides.** A user can override discovery with
  `--config /path/to/personal.yaml`. No additional layer for that.
- **New signal types or new YAML keys.** The schema is unchanged
  from slice 2: `render:`, `codeshape:`, `local_clone_dir:`.
- **Renaming the config file.** Still `pr-analyzer.yaml`
  everywhere — in the repo, in `$XDG_CONFIG_HOME/pr-analyzer/`,
  in `$HOME/.config/pr-analyzer/`. One filename, one mental model.

## Schema

Unchanged from slice 2. The same YAML shape is parsed regardless of
where the file was found:

```yaml
local_clone_dir: ./checked-out-repo   # absolute, or relative to THIS YAML file's directory

render:
  bar_scale: 100

codeshape:
  risky_paths:
    - security
    - auth/identity
  max_loc: 1000
  languages:
    preferred: [Go, TypeScript]
    allowed:   [Go, TypeScript, Rust]
```

### `local_clone_dir` when the config lives at a user-level path

`local_clone_dir` resolves relative to the YAML file's directory
(slice 3's rule). When the YAML lives at
`$HOME/.config/pr-analyzer/pr-analyzer.yaml`, a relative
`local_clone_dir: ./repo` resolves to
`$HOME/.config/pr-analyzer/repo` — which is almost never what the
user wants.

This is a soft footgun, not a hard error. The loader resolves the
path mechanically; the downstream collector that consumes
`local_clone_dir` errors at point-of-use if the directory doesn't
contain a checkout. The fix the user reaches for is one of:

- Set an absolute path in the user-level config.
- Set `local_clone_dir` per-repo (via a repo-local YAML, which
  takes precedence in discovery anyway).
- Pass `--local-clone-dir` on the CLI, which trumps the YAML value.

The slice intentionally does not try to be clever about this. A
warning could be added later if a real footgun report appears.

## Discovery and precedence

`internal/configfile/Discover(startDir)` resolves the org config via
this ladder. The first source that exists wins; later sources are
not consulted.

1. **CWD walk-up.** Walk from `startDir` upward, looking for
   `pr-analyzer.yaml`, stop at the filesystem root. This is the
   existing slice-2 behavior, unchanged.
2. **`$XDG_CONFIG_HOME/pr-analyzer/pr-analyzer.yaml`** if
   `XDG_CONFIG_HOME` is set and the file exists.
3. **`$HOME/.config/pr-analyzer/pr-analyzer.yaml`** if `HOME` is
   set and the file exists. (Per the XDG Base Directory
   Specification, this is the implied default when `XDG_CONFIG_HOME`
   is unset.)
4. **Nothing found.** Return a zero `analyzer.Config`, empty path,
   no warnings — same posture as slice 2's "no config in walk-up."

The `--config <path>` CLI flag bypasses discovery entirely. Missing
file at an explicit `--config` path is fatal.

### Why walk-up before XDG, not the reverse

Repo-local config is more specific than user-level config. A
contributor cloning an OSS project that ships its own
`pr-analyzer.yaml` should see the project's rules, not whatever
their corp / personal config happens to say. Walk-up matches the
contributor's expectation; XDG is the fallback for repos that don't
ship one.

## Loader posture

Unchanged from slice 2:

- Unparseable YAML at any discovered path: fatal.
- Unknown keys / wrong-type values: warnings, the recognized
  portion of the file still loads.
- Missing file at a discovery path: silent fall-through to the
  next source.
- Missing file at an explicit `--config` path: fatal.

## Library API

`analyzer.Config` and `analyzer.WithConfig` are unchanged.

`internal/configfile.Load` is unchanged.

`internal/configfile.Discover(startDir)` is unchanged in signature.
Its body adds the XDG / HOME fallbacks after the existing walk-up
loop. The returned path is the source that won, so
`cmd/pr-analyzer` can continue to relay it (e.g. in error messages
or `--verbose` traces) without changes.

A helper inside `internal/configfile` computes the user-level
candidate path:

```go
// userConfigPath returns the user-level org-config path the
// discovery ladder should try, or "" if neither XDG_CONFIG_HOME nor
// HOME is set. Pure function over the two env values; exported only
// for tests.
func userConfigPath(xdgConfigHome, home string) string
```

The CLI binary reads the env via `os.Getenv` and passes both values
in, so tests can drive the function with explicit inputs.

## Module impact

- `internal/configfile/configfile.go`: `Discover` body extended with
  XDG / HOME fallback; one new helper (`userConfigPath`).
- `cmd/pr-analyzer/main.go`: unchanged. Already calls
  `configfile.Discover(cwd)`.
- No fixture changes; tests construct YAML inline in temp dirs.
- No analyzer / codeshape / renderer changes.

## Test plan (TDD)

1. **`userConfigPath` happy path.** With
   `xdgConfigHome="/tmp/x"`, returns
   `/tmp/x/pr-analyzer/pr-analyzer.yaml`. With `xdgConfigHome=""`
   and `home="/tmp/h"`, returns
   `/tmp/h/.config/pr-analyzer/pr-analyzer.yaml`. With both empty,
   returns `""`.
2. **`Discover` finds XDG path.** Set `XDG_CONFIG_HOME=$TMP`, write
   `$TMP/pr-analyzer/pr-analyzer.yaml`, call `Discover` with a
   `startDir` that has no walk-up hits. Assert the XDG path
   wins.
3. **`Discover` finds HOME path.** `XDG_CONFIG_HOME` unset,
   `HOME=$TMP`, write
   `$TMP/.config/pr-analyzer/pr-analyzer.yaml`. Assert the HOME
   path wins.
4. **Walk-up takes precedence over XDG.** Project YAML in `startDir`,
   XDG YAML present. Walk-up wins; XDG is not consulted.
5. **Nothing found is silent.** Both env vars unset (or pointing at
   empty dirs) and no walk-up hit. Returns zero Config, empty path,
   no warnings, no error.
6. **`--config` overrides everything.** Explicit path passed via
   the CLI flag bypasses discovery — verified through the existing
   `Load(path)` call site in `cmd/pr-analyzer`. Existing
   `TestKongParse_Config_*` smoke coverage is sufficient; no new
   test needed unless a regression surfaces.
7. **Smoke test from `$HOME` discovery.** Drop a YAML at
   `$HOME/.config/pr-analyzer/pr-analyzer.yaml`, run the binary
   from a subdirectory that has no walk-up hit, scan a fixture PR,
   assert the user-level YAML's signals appear (e.g. a configured
   `risky_paths` matches a fixture file).

## Resolved decisions

- **One config, multiple discovery sources.** Layering between two
  files of the same kind is what "Option B" of the design
  conversation would have been; rejected as too complex for the
  current use case. If a corp publishes rules and an OSS project
  also has opinions, the user picks one (the walk-up wins when
  inside the OSS checkout; the user-level config applies elsewhere).
- **Same filename everywhere.** `pr-analyzer.yaml` in the repo, in
  XDG, in HOME. One mental model.
- **Walk-up beats XDG.** Repo-local intent is more specific than
  user-level default. An OSS project's `pr-analyzer.yaml` is the
  authoritative source for that repo's scans.
- **`local_clone_dir` from a user-level YAML is a soft footgun.**
  Documented; not solved. The path resolves against the YAML's
  directory mechanically; downstream collectors fail loud at
  point-of-use if the resulting path isn't a checkout.
- **No `--org-config` flag.** `--config` already exists and does
  the same job (explicit-path override of discovery). Adding a
  second flag would imply two distinct concepts; there's only one.
- **No remote sources.** Plain file paths. HTTPS / Git refs come
  with caching, auth, schema-version pinning — none of which is
  earned by current usage.

## Non-goals reminder

The slice does not change what pr-analyzer signals or how those
signals are surfaced. It only changes where the *one* org config
might live on disk. "Emit, don't enforce" continues unchanged — an
org's config produces pills in the report; pr-analyzer never blocks
merge, never exits non-zero on a signal hit, never notifies anyone.

## Migration notes

- Slice-5 behavior is preserved: a project that ships its own
  `pr-analyzer.yaml` in the repo continues to work exactly as
  today. Walk-up discovery is unchanged in priority and semantics.
- A user-level YAML is purely additive — installing one at
  `~/.config/pr-analyzer/pr-analyzer.yaml` only affects scans
  whose CWD does not have a walk-up hit. Existing project-local
  configs win in their own checkouts.
- The existing slice-5 smoke tests
  (`TestSmoke_RepoList`, `TestSmoke_Inspect`, `TestSmoke_RenderHTML`)
  must continue to pass unchanged — they don't reference user-level
  config, so the discovery's "no XDG / HOME match" path must
  produce identical behavior to today.
