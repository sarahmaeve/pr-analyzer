# Third-Slice Prototype: local clone directory

## Goal

Thread a "where on disk is this PR's checked-out repository?" path through pr-analyzer. The path is read-only and **pr-analyzer never clones, fetches, or writes to it** — the user (or the importing program) is responsible for the checkout existing. Slice 3 is foundational plumbing; the first collector that *uses* the path lands in a later slice (engineer-profile signals from OWNERS-style files).

## In scope

- `analyzer.Config` gains a `LocalCloneDir string` field.
- `pr-analyzer.yaml` gains a top-level `local_clone_dir:` key.
- CLI gains `--local-clone-dir <path>` flag with Kong's `type:"existingdir"` validation.
- Relative paths resolve differently depending on where they came from:
  - **CLI flag → resolved against CWD.**
  - **YAML value → resolved against the directory containing the YAML file.**
- Precedence in the CLI: `--local-clone-dir` (CLI) > `local_clone_dir` (YAML) > CWD.
- Library importers set `Config.LocalCloneDir` directly; the library does not impose a default.
- Tests verify each source, each resolution rule, and the precedence ladder.

## Out of scope (deferred)

- Reading OWNERS / CODEOWNERS files. Slice 4+.
- Any collector signal that consumes `LocalCloneDir`. Slice 4+.
- Cloning, fetching, or validating that the path is a git checkout. The directory is opaque — collectors that need git metadata can ask later.
- Multiple repos / sub-repo configuration.
- Renderer changes. Slice 3 ships no observable signal changes; the renderer is untouched.

## Naming rationale

The name avoids the `_loc` / `LOC` collision with **L**ines **O**f **C**ode, which already appears throughout the codebase (`codeshape.LOC`, `MaxLOC`, the bar's scale). `local_clone_dir` makes the intent unambiguous: a local checkout (the user clones; we read).

## Schema

```yaml
local_clone_dir: ./checked-out-repo   # absolute, or relative to THIS YAML file's directory
render: { ... }
codeshape: { ... }
```

## CLI surface

```go
type cliArgs struct {
    Config        string `short:"c" type:"existingfile" help:"Path to org config file."`
    LocalCloneDir string `name:"local-clone-dir" type:"existingdir" help:"Local checkout of the PR's repository. Defaults to CWD when neither --local-clone-dir nor local_clone_dir is set."`
    PR            string `arg:"" name:"pr-ref" help:"PR ref: owner/repo#number or full GitHub PR URL."`
}
```

Kong's `type:"existingdir"` validates that the directory exists before `run()` is called — bad paths fail at flag-parse time with a clear message. Relative paths supplied via the flag are resolved (by Kong's `existingdir` check) against the **current working directory** of the running CLI.

## Relative-path resolution

Two resolution bases. The choice depends on where the relative path was specified:

| Source | Base for relative paths | Rationale |
|---|---|---|
| `--local-clone-dir` (CLI flag) | CWD | Matches user expectations from the shell — the path the user typed is interpreted where they typed it. |
| `local_clone_dir` (YAML) | directory containing the YAML file | The config file describes the layout *around itself*; relative paths in it mean "relative to this file". Same rule as `.gitignore`, `.golangci.yml`, etc. |
| `analyzer.Config.LocalCloneDir` set programmatically | left as-is | Library leaves untouched. Importer decides. |

In practice the two resolution bases will often produce the same absolute path (when the user runs the CLI from the directory containing the YAML), but they diverge cleanly when the user runs from somewhere else.

Implementation:

- `configfile.Load(path)`: after decoding, if `cfg.LocalCloneDir` is non-empty and not absolute, prepend `filepath.Dir(path)`.
- CLI `run()`: after Kong parsing, if `args.LocalCloneDir` is non-empty and not absolute, prepend CWD (`os.Getwd()`).
- The resolved absolute path is what lands in `analyzer.Config.LocalCloneDir`.

## Precedence (resolved in CLI `run()`)

1. `--local-clone-dir` set on the CLI → use it (Kong validated existence; we resolve against CWD if relative).
2. Else `local_clone_dir:` set in the loaded YAML → use it (already resolved against the YAML's directory by the loader).
3. Else CWD via `os.Getwd()`.

The resolved path is written into `analyzer.Config.LocalCloneDir` and threaded through `analyzer.WithConfig`.

## Library API

`analyzer.Config.LocalCloneDir` is exported. Embedders set it on construction:

```go
cfg := analyzer.Config{
    LocalCloneDir: "/path/to/checkout",
    // ... CodeShape, Render, etc.
}
analysis, err := analyzer.Analyze(ctx, src, ref, analyzer.WithConfig(cfg))
```

The library does **not** default `LocalCloneDir` and does not validate it. Embedders that omit it get `""`. Downstream collectors that need the path are responsible for checking existence before they scan files — when they need it, they get a clear error if the path is missing, empty, or doesn't point at a directory.

## Loader posture

- Unset / missing `local_clone_dir` → `LocalCloneDir` stays `""`.
- Wrong type (e.g. `local_clone_dir: 42`) → warning emitted via the existing lenient path; field defaults to `""`.
- Path doesn't exist on disk → loader does **not** stat. Validation lives in Kong (for the CLI flag) and at point-of-use (for downstream collectors).
- Relative path → resolved against the YAML's directory at load time; the resulting absolute path is what lands on `Config`.

## Module impact

- `analyzer/config.go`: new `LocalCloneDir string` field with `yaml:"local_clone_dir"` tag.
- `internal/configfile/configfile.go`: after Unmarshal, resolve a relative `Config.LocalCloneDir` against `filepath.Dir(path)`.
- `cmd/pr-analyzer/main.go`: Kong struct gains `LocalCloneDir`. `run()` applies the precedence ladder, resolves a relative flag value against CWD, writes the final absolute path into the `Config` passed to `Analyze`.
- No new packages, no new collectors, no renderer changes, no smoke-test changes (output stays identical for an empty `local_clone_dir`, which is the slice-2 baseline).

## Test plan (TDD, patterns from slice-1 and slice-2 audits)

1. **YAML loader picks up `local_clone_dir`.** Add a case to `TestLoad_HappyPath` for an absolute-path value — decoded straight into `Config.LocalCloneDir`.
2. **YAML loader resolves a relative path against the YAML's directory.** Write a config at `<tempDir>/pr-analyzer.yaml` with `local_clone_dir: subdir`, after `Load` the field should equal `<tempDir>/subdir` (absolute).
3. **Kong parses `--local-clone-dir`.** `kong.New + parser.Parse(["--local-clone-dir", tempDir, "o/r#1"])`. Assert `cli.LocalCloneDir` matches.
4. **Kong rejects non-existent `--local-clone-dir`.** `parser.Parse(["--local-clone-dir", "/does/not/exist", "o/r#1"])` returns an error mentioning the path.
5. **CLI flag with relative path resolves against CWD.** Cover this in the `run()` precedence test below — relative `--local-clone-dir` becomes `filepath.Join(cwd, raw)`.
6. **Precedence in `run()`.** Three sub-cases:
   - flag set + YAML set → flag wins.
   - flag empty, YAML set → YAML wins.
   - flag empty, YAML empty → CWD.
7. **Library passthrough.** Unit test: `analyzer.Analyze(..., WithConfig(Config{LocalCloneDir: "/foo"}))` carries the value through. Pins the contract; future regression in `Analyze`'s option handling can't silently drop the field.

## Resolved decisions

- **Name: `local_clone_dir` / `--local-clone-dir` / `LocalCloneDir`** — avoids the `_loc`/`LOC` (Lines Of Code) collision in this codebase.
- **Dual relative-path resolution** — CLI flag values against CWD; YAML values against the YAML file's directory. Matches user shell expectations and standard tooling convention respectively.
- **Top-level `Config.LocalCloneDir`** rather than a nested `Config.Repo.Location` — single field, simplest surface.
- **Lenient YAML loader, strict CLI validation.** Kong catches typos / missing paths at flag-parse time; YAML mistakes get warnings but never block. Existence verified at point-of-use by downstream collectors.
- **No library default.** Embedders set the value explicitly or accept downstream collector errors when scanning is attempted.
- **No renderer change** in slice 3.

## Non-goals reminder

pr-analyzer is read-only against the local checkout. We never `git clone`, never `git fetch`, never write to the checkout. The user (or the importing program) is responsible for putting the right files at the right path before invoking us.
