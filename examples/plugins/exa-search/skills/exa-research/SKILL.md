---
name: exa-research
description: "Use Exa for current web research, deep dives, competitive analysis, lead discovery, literature reviews, and source-backed investigation. Trigger on requests to research, search the web, find current information, compare competitors, discover companies or people, or investigate a topic across multiple sources."
---

# Exa research

Use the approved Exa MCP tools for web research in this workspace.

## Tool choice

- Start with `web_search_exa` when the user needs current facts, sources, companies, people, news, comparisons, or broader discovery.
- Use a semantically rich query that describes the ideal result, not a short keyword list.
- Use `web_fetch_exa` after search when snippets are insufficient, or when the user gives one or more URLs whose full contents must be read.
- Batch URLs into one `web_fetch_exa` call when possible.

## Workflow

1. Clarify the target, time range, filters, and desired output when they materially change the search.
2. Search broadly enough for the requested depth, then discard results that do not meet the user's criteria.
3. Fetch the strongest sources when their full context is needed.
4. Deduplicate repeated URLs and cross-check important claims across independent sources.
5. Answer with linked sources and distinguish sourced facts from inference.

If Exa is unavailable or rate-limited, report the exact failure and the authentication or configuration needed. Do not silently substitute a different search provider when the request or task specifically requires Exa.
