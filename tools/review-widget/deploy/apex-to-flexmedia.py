#!/usr/bin/env python3
"""
Point hbhf.is and www.hbhf.is at Cloudflare's proxy so the existing
"hbhf.is -> flexmedia.is" Redirect Rule can actually fire.

Today both names resolve straight to HubSpot (unproxied), so Cloudflare never
sees the request and the rule never runs — HubSpot answers instead, redirecting
the apex to www and then serving a 404.

Changes, web records only:
  hbhf.is       A     199.60.103.69/.169  (HubSpot, unproxied)  ->  A 192.0.2.1 proxied
  www.hbhf.is   CNAME …hscoscdn-eu1.net   (HubSpot, unproxied)  ->  A 192.0.2.1 proxied

192.0.2.1 is TEST-NET-1 (RFC 5737) — a deliberately unroutable placeholder. The
origin is never contacted because the Redirect Rule answers at Cloudflare's edge
before any origin fetch, so the address only has to exist, not respond.

MX and TXT records are NOT touched. hbhf.is runs Google Workspace mail and SPF/
DMARC; proxying only affects HTTP, so mail keeps working.

Dry run by default. Pass --apply to write.
"""
import json
import os
import sys
import urllib.error
import urllib.request

ZONE = "0cc6ea2ad54761984832911fd1d74922"  # hbhf.is
TOKEN = os.environ.get("CF_TOKEN", "")
APPLY = "--apply" in sys.argv

PLACEHOLDER = "192.0.2.1"
TARGETS = ["hbhf.is", "www.hbhf.is"]

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

    listing = api("GET", f"{BASE}?per_page=100")
    if not listing.get("success"):
        sys.exit(f"could not list records: {listing.get('errors')}")

    records = listing["result"]

    # Safety: never touch anything that carries mail or verification.
    protected = [r for r in records if r["type"] in ("MX", "TXT", "NS", "CAA")]
    print(f"protected (untouched): {len(protected)} MX/TXT/NS/CAA records")
    for r in protected:
        print(f"    {r['type']:5} {r['name']}")
    print()

    web = [r for r in records if r["name"] in TARGETS and r["type"] in ("A", "AAAA", "CNAME")]
    if not web:
        print("no web records found for the target names — nothing to do")
        return

    print("web records to replace:")
    for r in web:
        print(f"    {r['type']:5} {r['name']:16} proxied={r['proxied']} -> {r['content']}")
    print()

    if not APPLY:
        print("DRY RUN — re-run with --apply to make these changes:")
        for name in TARGETS:
            print(f"    delete existing web records for {name}")
            print(f"    create  A {name} -> {PLACEHOLDER} (proxied)")
        return

    for name in TARGETS:
        for r in [x for x in web if x["name"] == name]:
            res = api("DELETE", f"{BASE}/{r['id']}")
            ok = res.get("success")
            print(f"  {'-' if ok else '!'} deleted {r['type']} {name} -> {r['content'][:40]}"
                  + ("" if ok else f"  FAILED: {res.get('errors')}"))

        res = api(
            "POST",
            BASE,
            {
                "type": "A",
                "name": name,
                "content": PLACEHOLDER,
                "proxied": True,
                "comment": "placeholder origin; Redirect Rule sends this to flexmedia.is",
            },
        )
        if res.get("success"):
            print(f"  + created A {name} -> {PLACEHOLDER} (proxied)")
        else:
            print(f"  ! create {name} FAILED: {res.get('errors')}")


if __name__ == "__main__":
    main()
