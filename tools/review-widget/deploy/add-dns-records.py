#!/usr/bin/env python3
"""
Create the hbhf.is DNS records for the parallel-hostname cutover.

  review-ingest.hbhf.is  CNAME -> <claude tunnel>.cfargotunnel.com  (proxied)
  workspace.hbhf.is      CNAME -> <claude tunnel>.cfargotunnel.com  (proxied)
  dart.hbhf.is           CNAME -> dart.flexmedia.is                 (proxied)

dart is NOT a tunnel hostname: the live ips app runs on the 1984.is VPS, and
dart.flexmedia.is already resolves to it through Cloudflare. Pointing the new
name at the old one keeps a single origin definition, so both hostnames serve
the same app and there is nothing to keep in sync.

Idempotent: existing records with the same name are left untouched and reported.
Nothing is deleted — flexmedia.is keeps working throughout.
"""
import json
import os
import sys
import urllib.request

ZONE = "0cc6ea2ad54761984832911fd1d74922"  # hbhf.is
TUNNEL = "c32108ee-133c-45b7-b333-424696b1f510"  # "claude"
TOKEN = os.environ.get("CF_TOKEN", "")

TUNNEL_TARGET = f"{TUNNEL}.cfargotunnel.com"

RECORDS = [
    ("review-ingest.hbhf.is", TUNNEL_TARGET),
    ("workspace.hbhf.is", TUNNEL_TARGET),
    ("dart.hbhf.is", "dart.flexmedia.is"),
]

BASE = f"https://api.cloudflare.com/client/v4/zones/{ZONE}/dns_records"


def api(method, url, body=None):
    data = json.dumps(body).encode() if body is not None else None
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Authorization", f"Bearer {TOKEN}")
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req) as r:
            return json.load(r)
    except urllib.error.HTTPError as e:
        return json.load(e)


def main():
    if not TOKEN:
        sys.exit("CF_TOKEN not set")

    existing = api("GET", f"{BASE}?per_page=100")
    if not existing.get("success"):
        sys.exit(f"could not list records: {existing.get('errors')}")
    by_name = {r["name"]: r for r in existing.get("result", [])}

    for name, target in RECORDS:
        if name in by_name:
            cur = by_name[name]
            print(f"  = {name} already exists ({cur['type']} -> {cur['content']}) — left alone")
            continue

        res = api(
            "POST",
            BASE,
            {
                "type": "CNAME",
                "name": name,
                "content": target,
                "proxied": True,
                "comment": "review widget / hbhf.is migration",
            },
        )
        if res.get("success"):
            print(f"  + {name} -> {target} (proxied)")
        else:
            errs = res.get("errors", [])
            print(f"  ! {name} FAILED: {errs}")


if __name__ == "__main__":
    main()
