# pr-analyzer

Mechanical PR analyzer. Emits Code Shape and Engineer Profile signals about a pull request; downstream tools (or humans) decide what to do with them.

## Build

```
go build ./cmd/pr-analyzer
```

Requires Go 1.26.

## Run

```
GITHUB_TOKEN=<token> ./pr-analyzer Kong/kong#14838
GITHUB_TOKEN=<token> ./pr-analyzer https://github.com/Kong/kong/pull/14838
```

Flags:

- `--config <path>` — project YAML config. Without it, walks up looking for `pr-analyzer.yaml`.
- `--local-clone-dir <path>` — local checkout for collectors that scan files on disk; defaults to CWD.

Example output:

```
PR #14838 bungle https://github.com/Kong/kong/pull/14838
[++-]
adds: 187  deletes: 7  files: 8
tests touched
no dependency manifest touched
languages: Lua, YAML
```

See `design/` for the concept doc and per-slice implementation specs.
