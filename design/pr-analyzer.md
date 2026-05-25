# Concept

This is a mechanical analyzer for changelists and pull requests.
It is inspired by circa-2011 Facebook PR tools, which displayed a quality signal of 100-200 changelists for each daily build. A similar evolution of a product is Phacility's Herald (https://www.phacility.com/phabricator/herald/) which is no longer maintained.

## Tools

Our code is in Go whenever possible.
We use test-driven development.
Our product does not directly query LLMs, but provides a signal for both other products and LLMs as needed.
We need to have the ability to ship this as a separate code module.

## Draft ideas

A PR (pull request) can be evaluated by several measures.

### Code Shape

- How many lines of code (LOCs) is the PR? Are those mostly additions? Are they mostly deletions?

- Does the PR have adequate test coverage?

- Is the PR adding a new dependency?

- Does the PR touch dangerous or risky areas of the codebase, like a billing platform, a core data storage layer, business or serving logic, etc.?

### Engineer Profile

- What is the tenure of this engineer at the company? Are they an intern? Did they just join?

- For an open source project, has this engineer had a PR accepted already?

- Do we have any signal as to the diligence, quality, or bona fides of this engineer?

- Is this engineer in an OWNERS file or similar entry that indicates they are competent and responsible for this area of the project?

- Is engineer on a restricted list, where their work needs to be further scrutinized because of previous behavior? (Circa-2011, this was the internal Facebook "karma" system)

### Org Ruleset

(An "org" here is any entity that publishes config — an OSS project, a corporation, a team. The same config shape applies regardless of who's doing the publishing.)

- Does the org have a preferred or mandatory set of programming languages or technologies? Does the PR stick to those?

- Is the PR using banned languages or technologies? Implementing in ways that are identified as antipatterns?

- Is the PR larger than the maximum LOC size the org has set?

## Customizable

We need to allow users to customize and connect to other sources of information. For example, a user may want to query their own employee database to answer questions about an engineer's tenure.

For open-source software projects, which we can dogfood test, we should define these sub-connectors as configurable elements. For example, we should ship with support for GitHub pull requests.

An org may have specific needs for linters or testing, and we will need to allow them to configure that -- or at a minimum, not block users from importing our module and using it as a base.

## Output

We should start with two output mechanisms:
- a single CLI visibility tool, that analyzes a given PR, and makes the state of that PR visible as a horizontal bar showing LOC size, with info below.

An example:

PR #113 Sarah M <email> <link>
[+++++-----]
Sarah is in the OWNERS file.
This code has no unusual shapes.
This code follows product restrictions.
13 of Sarah's previous PRs have been accepted, the last merged at DD-MM-YY <link>

where the simple bar graph [++++-----] refers to lines modified, divided into adds and deletes. The scale of this graph should be configurable by org; our initial default should be 100 LOCs per glyph.

## Status

Implementation is sliced; per-slice specs in `design/PROTO*.md`.

- **Slice 1** ([PROTO.md](PROTO.md)): end-to-end Code Shape pipeline (LOC, tests, manifests, languages), GitHub connector, CLI renderer.
- **Slice 2** ([PROTO2.md](PROTO2.md)): org YAML config — risky paths, max LOC, language posture. "Org" here is the unified term for whatever entity publishes the config (an OSS project, a corporation, etc.).
- **Slice 3** ([PROTO3.md](PROTO3.md)): local clone directory plumbing (`--local-clone-dir` / `local_clone_dir`), foundational for future OWNERS / git-log collectors.
- **Slice 4** ([PROTO4.md](PROTO4.md)): Engineer Profile foundation — `author_association` signal end-to-end, metadata-only, no new API calls. Heavier engineer-profile work (OWNERS, git log) lands in a later slice and consumes the slice-3 local clone directory.
- **Slice 5** ([PROTO5.md](PROTO5.md)): repo-wide scan + HTML / JSON renderers. Bare `owner/repo` argument lists open PRs; output is a self-contained HTML report (cinematic sci-fi palette, chamfered cards, blue/green LOC bar + muted files-changed bar, color-coded severity pills) and a sibling `analyses.json` machine-readable envelope. Two new subcommands: `inspect <analyses.json>` (text summary), `render-html <analyses.json>` (re-render without re-fetching). Generalized auth-token check (`internal/credentials`) and a 300-500ms randomized rate limiter for list mode.
- **Slice 6** ([PROTO6.md](PROTO6.md), specced — not yet implemented): org config discovery from XDG / `$HOME` paths. Lets an org publish `pr-analyzer.yaml` once at a shared user-level location instead of dropping a copy into every repo. One config, multiple discovery sources, first-match-wins. No layering.
- **Agent-config-touched signal** (between slices 3 and 4): cross-project threat class — PRs adding `.cursorrules`, `CLAUDE.md`, `.claude/`, `.cursor/`, `.github/copilot-instructions.md`, etc. Cross-references signatory's Trapdoor crypto-stealer threat-landscape entry.
- **Catalog additions**: Lua programming language; Copilot custom-instructions paths; Gemini configuration.

Live test targets: `sarahmaeve/signatory#144` (real, slice 1 baseline), the fabricated `agentforge/copilot-toolkit#47` Trapdoor fixture (`connectors/github/testdata/pr_trapdoor*.json`), and `atuinsh/atuin` (slice 5 dogfood — 83 open PRs, full HTML + JSON artifact pair).
