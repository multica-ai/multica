from __future__ import annotations

import argparse
import asyncio

from .acp import ACPServer, DEFAULT_MODEL
from .runner import CodeModeRunner


def main() -> None:
    parser = argparse.ArgumentParser(prog="pydantic-acp-agent")
    parser.add_argument("command", choices=["acp"], help="Run the ACP stdio server")
    parser.add_argument("--model", default=None, help=f"Pydantic AI model id. Default: {DEFAULT_MODEL}")
    args = parser.parse_args()

    server = ACPServer(CodeModeRunner(model=args.model))
    asyncio.run(server.serve())


if __name__ == "__main__":
    main()
