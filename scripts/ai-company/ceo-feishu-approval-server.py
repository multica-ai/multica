#!/usr/bin/env python3
"""Feishu event server for CEO approval cards and /批 commands."""

from __future__ import annotations

import argparse
import json
import os
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent
sys.path.insert(0, str(SCRIPT_DIR / "lib"))

from approval_bridge import (  # noqa: E402
    AI_COMPANY_ROOT,
    handle_feishu_card_action,
    handle_feishu_text_message,
)


def extract_message_text(event: dict[str, Any]) -> str:
    message = event.get("message") or {}
    content_raw = message.get("content") or "{}"
    try:
        content = json.loads(content_raw)
    except json.JSONDecodeError:
        return ""
    return str(content.get("text") or "").strip()


class FeishuApprovalHandler(BaseHTTPRequestHandler):
    registry_path: Path = AI_COMPANY_ROOT / "templates" / "project-registry.yaml"
    github_org: str = "chenzh"
    verification_token: str = ""

    def log_message(self, fmt: str, *args: Any) -> None:
        sys.stderr.write("%s - %s\n" % (self.address_string(), fmt % args))

    def _read_json(self) -> dict[str, Any]:
        length = int(self.headers.get("Content-Length") or "0")
        raw = self.rfile.read(length) if length else b"{}"
        return json.loads(raw.decode("utf-8"))

    def _write_json(self, payload: dict[str, Any], status: int = 200) -> None:
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802
        if self.path in {"/", "/health"}:
            self._write_json({"ok": True, "service": "ceo-feishu-approval"})
            return
        self._write_json({"error": "not found"}, status=404)

    def do_POST(self) -> None:  # noqa: N802
        if self.path not in {"/feishu/event", "/"}:
            self._write_json({"error": "not found"}, status=404)
            return

        body = self._read_json()
        if "challenge" in body:
            self._write_json({"challenge": body["challenge"]})
            return

        token = str(body.get("token") or body.get("header", {}).get("token") or "")
        if self.verification_token and token != self.verification_token:
            self._write_json({"error": "invalid token"}, status=401)
            return

        event_type = str(body.get("header", {}).get("event_type") or body.get("type") or "")
        event = body.get("event") or {}

        try:
            if event_type == "card.action.trigger":
                action = event.get("action") or {}
                value = action.get("value") or {}
                response = handle_feishu_card_action(value)
                self._write_json(response)
                return

            if event_type == "im.message.receive_v1":
                text = extract_message_text(event)
                reply = handle_feishu_text_message(text, self.registry_path, self.github_org)
                if reply:
                    self._write_json({"msg": reply})
                    return
                self._write_json({})
                return
        except SystemExit as exc:
            self._write_json({"toast": {"type": "warning", "content": str(exc)}}, status=200)
            return
        except Exception as exc:  # noqa: BLE001
            self._write_json({"toast": {"type": "warning", "content": f"处理失败: {exc}"}}, status=200)
            return

        self._write_json({})


def main() -> int:
    parser = argparse.ArgumentParser(description="Feishu CEO approval callback server")
    parser.add_argument("--host", default=os.environ.get("CEO_FEISHU_APPROVAL_HOST", "127.0.0.1"))
    parser.add_argument("--port", type=int, default=int(os.environ.get("CEO_FEISHU_APPROVAL_PORT", "9478")))
    parser.add_argument(
        "--registry",
        default=os.environ.get(
            "REGISTRY",
            str(AI_COMPANY_ROOT / "templates" / "project-registry.yaml"),
        ),
    )
    parser.add_argument("--github-org", default=os.environ.get("GITHUB_ORG", "chenzh"))
    parser.add_argument("--verification-token", default=os.environ.get("FEISHU_VERIFICATION_TOKEN", ""))
    args = parser.parse_args()

    FeishuApprovalHandler.registry_path = Path(args.registry)
    FeishuApprovalHandler.github_org = args.github_org
    FeishuApprovalHandler.verification_token = args.verification_token

    server = ThreadingHTTPServer((args.host, args.port), FeishuApprovalHandler)
    print(f"ceo-feishu-approval-server listening on http://{args.host}:{args.port}/feishu/event", file=sys.stderr)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nstopped", file=sys.stderr)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
