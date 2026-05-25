# pr-analyzer

Mechanical PR analyzer. Emits Code Shape and Engineer Profile signals about pull requests; downstream tools (or humans) decide what to do with them.

## Build

```
go build ./cmd/pr-analyzer
```

Requires Go 1.26.

## Run

### Single PR — bar + bullets to stdout

```
GITHUB_TOKEN=<token> ./pr-analyzer Kong/kong#14838
GITHUB_TOKEN=<token> ./pr-analyzer https://github.com/Kong/kong/pull/14838
```

Output:

```
PR #14838 bungle https://github.com/Kong/kong/pull/14838
[++-]
adds: 187  deletes: 7  files: 8
tests touched
no dependency manifest touched
languages: Lua, YAML
```

### Repo-wide scan — HTML report + JSON envelope

```
GITHUB_TOKEN=<token> ./pr-analyzer --out=./report atuinsh/atuin
```

`GITHUB_TOKEN` is required in repo-wide mode; the binary errors out up front otherwise. Output goes to `<--out>/index.html` (self-contained, cinematic palette, JSON inlined for downstream consumers) and `<--out>/analyses.json` (machine-readable envelope, stable `schema_version`). Both artifacts are produced from a single in-memory envelope, so they cannot drift. Requests are rate-limited at 300-500 ms randomized per call.

### Inspect a saved scan — text summary

```
./pr-analyzer inspect ./report/analyses.json
```

Prints author-association distribution, languages, LOC stats with top-5 by size, tests-touched count, dependency manifests, and any agent-config / risky-paths touches.

### Re-render HTML from a saved scan

```
./pr-analyzer render-html ./report/analyses.json > ./report/index.html
```

Useful while iterating on the renderer — re-render against a cached scan without re-fetching the PRs over the rate-limited API path.

## Configuration

pr-analyzer reads a single YAML file — the **org config** — that drives the
collectors and the renderer. "Org" is the unified term for whatever entity
publishes the config: an OSS project, a corporation, a team within a
corporation. The same schema applies regardless of who's publishing it.

### Discovery ladder

The loader resolves which file to read via this ladder. First match wins;
later sources are not consulted.

1. **`--config <path>`** — explicit flag. Missing file at this path is fatal.
2. **CWD walk-up** — walks upward from CWD looking for `pr-analyzer.yaml`, stopping at the filesystem root.
3. **`$XDG_CONFIG_HOME/pr-analyzer/pr-analyzer.yaml`** — when the env var is set.
4. **`$HOME/.config/pr-analyzer/pr-analyzer.yaml`** — the XDG-default fallback.
5. **Nothing found** — silent; pr-analyzer runs with built-in defaults.

The walk-up beats the user-level paths because repo-local intent is more
specific than user-level default: a contributor inside an OSS checkout that
ships its own `pr-analyzer.yaml` sees that project's rules regardless of
whatever they have at `~/.config`. There is **no merging or layering**
between sources.

### YAML schema

All keys are optional. A file with no recognized keys produces no warnings
and runs with built-in defaults (slice-1 behavior).

```yaml
# Path to a local checkout of the PR's repository. Slice-3 plumbing for
# future collectors that scan files on disk (OWNERS, git log). Resolves
# relative to THIS YAML file's directory.
local_clone_dir: ./checked-out-repo

# Renderer / output-interface concerns.
render:
  # Override the CLI bar's default 100 LOC/glyph. Clamped to [100, 1000];
  # auto-scale rules still apply on top.
  bar_scale: 100

# Code Shape collector tuning.
codeshape:
  # Path-segment prefixes marking high-blast-radius areas of the codebase.
  # A pattern P matches a PR file path F if P == F or P + "/" is a prefix
  # of F. No wildcards. PRs touching these surface a RISKY PATHS pill
  # (orange) in the HTML drill-down.
  risky_paths:
    - billing
    - payments
    - auth/identity

  # Maximum total LOC (additions + deletions) before the renderer adds
  # an EXCEEDS MAX LOC pill (red). Unset / 0 = no opinion. pr-analyzer
  # never blocks — this is a signal, not a gate.
  max_loc: 1000

  # Language posture. Programming languages detected outside both lists
  # surface as ANOMALOUS LANGUAGES (orange). Languages in `preferred`
  # surface as PREFERRED LANGUAGES (green). Empty lists = no opinion;
  # the Languages bullet stays neutral.
  languages:
    preferred: [Go]
    allowed:   [Go, TypeScript]
```

### Loader posture

User files are **inputs**, not contracts. The loader is lenient:

- **Unparseable YAML** (broken syntax): fatal, with line / column in the error.
- **Missing `--config` target**: fatal.
- **Missing file at a discovery source**: silent fall-through to the next source.
- **Unknown key** (e.g. `codshape:` typo): warning with line number; key ignored, rest of file loads.
- **Wrong type for a known key** (e.g. `max_loc: "not a number"`): warning; that key uses its zero-value default; rest of section still loads.
- **`bar_scale` outside `[100, 1000]`**: warning, clamped to the nearest endpoint.

Warnings print to stderr; the run continues.

## Flags

| Flag | Applies to | Description |
|---|---|---|
| `--config <path>` | `scan` | Org YAML config. Bypasses discovery entirely. See **Configuration** for the discovery ladder. |
| `--local-clone-dir <path>` | `scan` | Local checkout for collectors that scan files on disk. Defaults to CWD. |
| `--out <dir>` | `scan` (list mode only) | Output directory for `index.html` and `analyses.json`. Defaults to `.`. Created if missing. Ignored in single-PR mode. |

## Subcommands

| Verb | Purpose |
|---|---|
| `scan <pr-ref>` *(default)* | Analyze one PR or all open PRs in a repo. The verb is optional — `pr-analyzer owner/repo[#N]` keeps working. `<pr-ref>` accepts `owner/repo` (list mode), `owner/repo#N`, or a full GitHub PR URL. |
| `inspect <analyses.json>` | Print a deterministic text summary of a previously-generated `analyses.json` (author-association distribution, languages, LOC stats with top-5 by size, tests-touched count, dependency manifests, agent-config touches). |
| `render-html <analyses.json>` | Re-render an HTML report to stdout from a previously-generated `analyses.json`. Lets you iterate on the renderer (CSS, template) against a cached scan without re-fetching from GitHub. Pipe to a file: `pr-analyzer render-html scan/analyses.json > scan/index.html`. |

## Environment variables

| Variable | Required | Purpose |
|---|---|---|
| `GITHUB_TOKEN` | List mode (`owner/repo`); optional for single-PR | Authenticates GitHub API calls. List mode hard-requires it before any network activity — the binary errors out up front otherwise, since GitHub's anonymous 60-req/hour budget can't service even a small repo. |
| `GITHUB_API_BASE_URL` | No | Overrides the default `https://api.github.com`. Loopback hosts and `https://api.github.com` only; arbitrary hosts are rejected to prevent the `GITHUB_TOKEN` from being sent to an attacker-controlled URL. Used by the test suite against `httptest.NewServer`. |
| `HOME` | No | Used by the discovery ladder above to locate `$HOME/.config/pr-analyzer/pr-analyzer.yaml` when no walk-up hit is found. |

## Design

See `design/` for the concept doc (`pr-analyzer.md`) and per-slice implementation specs (`PROTO.md` through `PROTO6.md`). Each slice ships an end-to-end change with a TDD-driven test pass.
