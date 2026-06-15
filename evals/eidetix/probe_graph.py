#!/usr/bin/env python3
"""
Eidetix graph probe — GATE A for the with/without eval.

Purpose: confirm the real partner graph behind a token is actually populated,
and dump what it returns for representative marketing queries. The eval is only
valid if `recall`/`search` return substantive, factual content — if the graph
is empty/sparse, the "with Eidetix" arm has nothing to add and the whole A/B
measures ~0 lift regardless of the integration's quality.

This also seeds the eval task suite: the queries that return rich facts tell us
which tasks the graph can actually help with.

Usage:
    pip install "mcp>=1.2"
    export EIDETIX_TOKEN='<your Marketing token>'    # the secret — never commit it
    python3 evals/eidetix/probe_graph.py

    # optional overrides:
    export EIDETIX_URL='https://eidetix.nodeops.xyz/mcp/sse'
    python3 evals/eidetix/probe_graph.py "brand voice" "pricing" "launch campaign"

The token is read only from the environment and is never printed.
"""

import asyncio
import json
import os
import sys

DEFAULT_QUERIES = [
    "brand voice and tone guidelines",
    "product positioning and key messaging",
    "target audience and personas",
    "past campaign decisions and outcomes",
    "competitors and differentiation",
]


def _truncate(obj, limit=1200):
    s = obj if isinstance(obj, str) else json.dumps(obj, indent=2, default=str)
    return s if len(s) <= limit else s[:limit] + f"\n... [truncated, {len(s)} chars total]"


async def main() -> int:
    token = os.environ.get("EIDETIX_TOKEN", "").strip()
    if not token:
        print("ERROR: set EIDETIX_TOKEN to the Marketing graph token (never commit it).")
        return 2
    url = os.environ.get("EIDETIX_URL", "https://eidetix.nodeops.xyz/mcp/sse").strip()
    queries = sys.argv[1:] or DEFAULT_QUERIES

    try:
        from mcp import ClientSession
        from mcp.client.streamable_http import streamablehttp_client
    except ImportError:
        print('ERROR: MCP SDK missing. Run: pip install "mcp>=1.2"')
        return 2

    headers = {"Authorization": f"Bearer {token}"}
    print(f"==> Connecting to {url} (streamable-http, Bearer auth)\n")

    try:
        async with streamablehttp_client(url, headers=headers) as (read, write, _):
            async with ClientSession(read, write) as session:
                await session.initialize()

                tools = await session.list_tools()
                names = [t.name for t in tools.tools]
                print(f"==> Tools advertised by the graph ({len(names)}): {names}\n")
                expected = {"recall", "search", "get_graph", "get_graph_expanded",
                            "get_content", "resolve_entities", "get_schema", "ingest_traces"}
                missing = expected - set(names)
                if missing:
                    print(f"    WARNING: expected tools not advertised: {sorted(missing)}\n")

                populated_hits = 0
                for q in queries:
                    tool = "recall" if "recall" in names else ("search" if "search" in names else None)
                    if tool is None:
                        print("ERROR: neither recall nor search is available; cannot probe content.")
                        return 1
                    print(f"--- {tool}({q!r}) ---")
                    try:
                        res = await session.call_tool(tool, {"query": q})
                        text = "\n".join(
                            getattr(c, "text", "") for c in res.content if getattr(c, "type", "") == "text"
                        ) or str(res.content)
                        if text.strip() and len(text.strip()) > 40:
                            populated_hits += 1
                        print(_truncate(text), "\n")
                    except Exception as e:  # noqa: BLE001 — probe wants to see any error verbatim
                        print(f"    call failed: {type(e).__name__}: {e}\n")

                print("=" * 60)
                verdict = "POPULATED" if populated_hits >= max(2, len(queries) // 2) else "SPARSE/EMPTY"
                print(f"VERDICT: {verdict} — {populated_hits}/{len(queries)} queries returned substantive content.")
                if verdict != "POPULATED":
                    print("If SPARSE/EMPTY: the real-graph eval cannot show lift. Either seed the graph "
                          "first (ingest team knowledge), or switch the eval's knowledge source.")
                return 0
    except Exception as e:  # noqa: BLE001
        print(f"ERROR connecting/initializing: {type(e).__name__}: {e}")
        print("If this is a transport error, the endpoint may speak SSE rather than streamable-http; "
              "try the mcp.client.sse.sse_client variant.")
        return 1


if __name__ == "__main__":
    raise SystemExit(asyncio.run(main()))
