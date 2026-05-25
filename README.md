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

## Flags

| Flag | Applies to | Description |
|---|---|---|
| `--config <path>` | `scan` | Org YAML config. Without it, walks up from CWD looking for `pr-analyzer.yaml`. |
| `--local-clone-dir <path>` | `scan` | Local checkout for future collectors that scan files on disk; defaults to CWD. |
| `--out <dir>` | `scan` (list mode only) | Output directory for `index.html` and `analyses.json`. Defaults to `.`. Ignored in single-PR mode. |

## Subcommands

| Verb | Purpose |
|---|---|
| `scan <pr-ref>` (default) | Analyze a single PR or all open PRs in a repo. The verb is optional — `pr-analyzer owner/repo[#N]` works. |
| `inspect <analyses.json>` | Print a deterministic text summary of a previously-generated `analyses.json`. |
| `render-html <analyses.json>` | Re-render an HTML report to stdout from a previously-generated `analyses.json`. |

## Design

See `design/` for the concept doc (`pr-analyzer.md`) and per-slice implementation specs (`PROTO.md` through `PROTO6.md`). Each slice ships an end-to-end change with a TDD-driven test pass.
