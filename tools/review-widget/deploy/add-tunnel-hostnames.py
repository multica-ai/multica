#!/usr/bin/env python3
"""
Add hbhf.is hostnames to the `claude` Cloudflare tunnel, alongside the existing
flexmedia.is ones (parallel-hostname cutover — nothing is removed).

Adds:
  review-ingest.hbhf.is -> http://localhost:8095   (review widget ingest)
  workspace.hbhf.is     -> http://localhost:3050   (Multica frontend)

Idempotent: re-running leaves the config unchanged.
Existing rules are preserved in order; the catch-all stays last.
"""
import json
import os
import sys
import urllib.request

ACCOUNT = "c0df0092fb8726e1b3edd215ef687906"
TUNNEL = "c32108ee-133c-45b7-b333-424696b1f510"  # "claude"
TOKEN = os.environ.get("CF_TOKEN", "")

NEW_RULES = [
    ("review-ingest.hbhf.is", "http://localhost:8095"),
    ("workspace.hbhf.is", "http://localhost:3050"),
]

BASE = f"https://api.cloudflare.com/client/v4/accounts/{ACCOUNT}/cfd_tunnel/{TUNNEL}/configurations"


def api(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {TOKEN}")
    req.add_header("Content-Type", "application/json")
    with urllib.request.urlopen(req) as r:
        return json.load(r)


def main():
    if not TOKEN:
        sys.exit("CF_TOKEN not set")

    current = api("GET", BASE)
    if not current.get("success"):
        sys.exit(f"could not read config: {current.get('errors')}")

    config = current["result"]["config"]
    ingress = config.get("ingress", [])

    existing = {r.get("hostname") for r in ingress if r.get("hostname")}
    additions = [(h, s) for h, s in NEW_RULES if h not in existing]

    if not additions:
        print("all hostnames already present — nothing to do")
        for h, _ in NEW_RULES:
            print(f"  = {h}")
        return

    # Keep the catch-all last: split it off, append, put it back.
    catch_all = [r for r in ingress if not r.get("hostname")]
    hosted = [r for r in ingress if r.get("hostname")]

    for hostname, service in additions:
        hosted.append({"hostname": hostname, "service": service, "originRequest": {}})
        print(f"  + {hostname} -> {service}")

    config["ingress"] = hosted + (catch_all or [{"service": "http_status:404"}])

    result = api("PUT", BASE, {"config": config})
    if not result.get("success"):
        sys.exit(f"update failed: {result.get('errors')}")

    print(f"\nupdated — {len(config['ingress'])} rules now configured")


if __name__ == "__main__":
    main()
