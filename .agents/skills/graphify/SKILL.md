---
name: graphify
description: Build and query a knowledge graph of this codebase so an AI coding assistant can answer architecture/where-is-X questions from a token-cheap subgraph instead of grepping and reading whole files. Use when asked to "map the codebase", "build the code graph", "graphify", "where does X connect", or before navigating an unfamiliar area of the monorepo.
metadata:
  author: safishamsi
  version: "0.8.40"
  source: https://github.com/safishamsi/graphify
  homepage: https://graphify.net/
  argument-hint: <path | question>
---

# Graphify — codebase knowledge graph

Graphify turns code, docs and diagrams into a queryable knowledge graph
(Tree-sitter AST extraction + NetworkX + Leiden clustering, no vector
embeddings). An assistant queries the graph for a small, relevant subgraph
instead of reading whole files — which cuts the tokens spent on
"how does this fit together" questions.

It is an external Python CLI, not a markdown-only skill. It must be installed
once per machine before use.

## Install (once per machine)

```bash
uv tool install graphifyy        # installs `graphify` + `graphify-mcp`
# or: pipx install graphifyy
```

Python 3.10+ required.

## Build the graph

```bash
graphify extract <path> --out <path>        # full build (AST + semantic + clustering)
graphify update  <path>                      # re-extract code only, no LLM, refresh existing graph
graphify extract <path> --no-cluster         # raw AST graph, fastest, no LLM
```

- **Code-only is fully offline** — Tree-sitter parses 36+ languages
  (Go, TS/JS, Python, SQL, JSON, …) with **no API key**.
- **Docs / PDFs / images need an LLM key** for semantic extraction, and the
  **community-naming / clustering** step also calls an LLM. Without a key,
  build with `--no-cluster` (or restrict to code-only paths) to stay offline.
  Set one of `ANTHROPIC_API_KEY` / `OPENAI_API_KEY` / `GEMINI_API_KEY` (etc.)
  only when you want named communities or doc extraction.

## Query the graph (the token-cheap path)

```bash
graphify query "<question>" --graph <path>/graphify-out/graph.json --budget 2000
graphify path "NodeA" "NodeB"      # shortest dependency path between two symbols
graphify explain "Symbol"          # plain-language summary of a node + neighbours
graphify affected "Symbol"         # reverse-traversal: what breaks if you change X
```

`query` returns nodes with `file:line` and caps output at `--budget` tokens —
that budget is the whole point: an architectural answer in ~2k tokens instead
of reading many files.

## firtal-cerebro specifics

- **Generated output is git-ignored.** `graphify extract`/`update` writes a
  `graphify-out/` directory (graph.json, graph.html, GRAPH_REPORT.md, cache).
  It is in `.gitignore` — **do not commit it.** A graph is derived state; it
  goes stale as code changes, so we rebuild on demand rather than version it.
- **Rebuild before querying** if the checkout changed since the last build.
  After a large refactor that deletes code, use `graphify update <path> --force`.
- **This is a big monorepo.** Scope the build to the area you care about
  (e.g. `graphify extract server/internal/cerebro --out .`) rather than the
  whole repo when you only need one subsystem — faster and cheaper.
- **No telemetry.** Graphify's only outbound call is the optional semantic
  step, which sends descriptions to your configured LLM, never raw source.

## When NOT to use it

When you already know the exact file/symbol, just open it. The graph pays off
for "how does this connect", "what depends on X", and onboarding into an
unfamiliar subsystem — not for a single known-location lookup.
