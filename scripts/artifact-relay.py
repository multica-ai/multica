#!/usr/bin/env python3
"""Cache verified Multica release assets and serve them from localhost.

This is an artifact relay, not a network proxy. `--sync` talks to GitHub,
downloads release metadata, checksums.txt, and release assets, verifies every
asset that appears in checksums.txt, and stores the result in a local cache.
`--serve` only serves cached metadata and cached files from 127.0.0.1.
"""

from __future__ import annotations

import argparse
import hashlib
import json
import os
import shutil
import sys
import tempfile
import urllib.error
import urllib.request
from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Dict, Iterable, Optional
from urllib.parse import urlparse


REPO = "multica-ai/multica"
GITHUB_API = "https://api.github.com"
DEFAULT_CACHE = Path.home() / ".cache" / "multica-artifact-relay"
CHECKSUMS_NAME = "checksums.txt"
HOST = "127.0.0.1"


class RelayError(Exception):
    pass


def request_json(url: str) -> dict:
    req = urllib.request.Request(
        url,
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "multica-artifact-relay",
        },
    )
    with urllib.request.urlopen(req, timeout=30) as resp:
        if resp.status != HTTPStatus.OK:
            raise RelayError(f"{url} returned HTTP {resp.status}")
        return json.loads(resp.read().decode("utf-8"))


def download(url: str, dest: Path) -> None:
    dest.parent.mkdir(parents=True, exist_ok=True)
    fd, tmp_name = tempfile.mkstemp(prefix=dest.name + ".", dir=str(dest.parent))
    os.close(fd)
    tmp = Path(tmp_name)
    try:
        req = urllib.request.Request(url, headers={"User-Agent": "multica-artifact-relay"})
        with urllib.request.urlopen(req, timeout=120) as resp:
            if resp.status != HTTPStatus.OK:
                raise RelayError(f"{url} returned HTTP {resp.status}")
            with tmp.open("wb") as out:
                shutil.copyfileobj(resp, out)
        tmp.replace(dest)
    except Exception:
        tmp.unlink(missing_ok=True)
        raise


def asset_filename(asset: dict) -> str:
    name = str(asset.get("name") or "").strip()
    if not name or "/" in name or name in {".", ".."}:
        raise RelayError(f"invalid release asset name: {name!r}")
    return name


def find_asset(release: dict, name: str) -> dict:
    for asset in release.get("assets", []):
        if asset.get("name") == name:
            return asset
    raise RelayError(f"release {release.get('tag_name')} has no {name}")


def parse_checksums(data: bytes) -> Dict[str, str]:
    checksums: Dict[str, str] = {}
    for raw_line in data.decode("utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        parts = line.split()
        if len(parts) < 2:
            continue
        checksum, filename = parts[0].lower(), parts[1]
        if len(checksum) == 64:
            checksums[filename] = checksum
    if not checksums:
        raise RelayError("checksums.txt did not contain any SHA-256 entries")
    return checksums


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            digest.update(chunk)
    return digest.hexdigest()


def verify_file(path: Path, expected: str) -> None:
    actual = sha256_file(path)
    if actual.lower() != expected.lower():
        path.unlink(missing_ok=True)
        raise RelayError(f"checksum mismatch for {path.name}: expected {expected}, got {actual}")


def release_dir(cache: Path, tag: str) -> Path:
    return cache / "releases" / tag


def assets_dir(cache: Path, tag: str) -> Path:
    return release_dir(cache, tag) / "assets"


def metadata_path(cache: Path, tag: str) -> Path:
    return release_dir(cache, tag) / "release.json"


def latest_path(cache: Path) -> Path:
    return cache / "latest.json"


def write_json(path: Path, data: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(data, indent=2, sort_keys=True), encoding="utf-8")
    tmp.replace(path)


def read_json(path: Path) -> dict:
    return json.loads(path.read_text(encoding="utf-8"))


def rewrite_release_urls(release: dict, public_base: str) -> dict:
    copied = json.loads(json.dumps(release))
    for asset in copied.get("assets", []):
        name = asset.get("name")
        if name:
            asset["browser_download_url"] = f"{public_base.rstrip('/')}/assets/{name}"
    return copied


def sync_release(cache: Path, tag: Optional[str]) -> str:
    if tag:
        api_url = f"{GITHUB_API}/repos/{REPO}/releases/tags/{tag}"
    else:
        api_url = f"{GITHUB_API}/repos/{REPO}/releases/latest"
    print(f"[sync] Fetching release metadata: {api_url}")
    release = request_json(api_url)
    tag_name = release.get("tag_name")
    if not tag_name:
        raise RelayError("release metadata is missing tag_name")

    target_assets = assets_dir(cache, tag_name)
    target_assets.mkdir(parents=True, exist_ok=True)

    checksums_asset = find_asset(release, CHECKSUMS_NAME)
    checksums_url = checksums_asset.get("browser_download_url")
    if not checksums_url:
        raise RelayError("checksums.txt is missing browser_download_url")

    checksums_file = target_assets / CHECKSUMS_NAME
    print(f"[sync] Downloading {CHECKSUMS_NAME}")
    download(checksums_url, checksums_file)
    checksums = parse_checksums(checksums_file.read_bytes())

    for asset in release.get("assets", []):
        name = asset_filename(asset)
        if name == CHECKSUMS_NAME:
            continue
        expected = checksums.get(name)
        if not expected:
            print(f"[sync] Skipping {name}: not present in {CHECKSUMS_NAME}")
            continue
        url = asset.get("browser_download_url")
        if not url:
            raise RelayError(f"{name} is missing browser_download_url")
        dest = target_assets / name
        print(f"[sync] Downloading {name}")
        download(url, dest)
        verify_file(dest, expected)
        print(f"[sync] {name}: verified OK")

    write_json(metadata_path(cache, tag_name), release)
    write_json(latest_path(cache), release)
    print(f"[sync] Cached {tag_name} in {release_dir(cache, tag_name)}")
    return tag_name


def safe_asset_path(cache: Path, filename: str) -> Path:
    if "/" in filename or filename in {"", ".", ".."}:
        raise RelayError("invalid asset path")
    latest = read_json(latest_path(cache))
    tag = latest.get("tag_name")
    if not tag:
        raise RelayError("latest release metadata is missing tag_name")
    path = assets_dir(cache, tag) / filename
    if not path.is_file():
        raise FileNotFoundError(filename)
    return path


class RelayHandler(BaseHTTPRequestHandler):
    cache: Path
    public_base: str

    def log_message(self, fmt: str, *args: object) -> None:
        sys.stderr.write("[serve] " + fmt % args + "\n")

    def send_json(self, payload: dict) -> None:
        data = json.dumps(payload).encode("utf-8")
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def send_error_text(self, status: HTTPStatus, message: str) -> None:
        data = (message + "\n").encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def do_GET(self) -> None:
        parsed = urlparse(self.path)
        path = parsed.path
        try:
            if path == f"/repos/{REPO}/releases/latest":
                self.send_json(rewrite_release_urls(read_json(latest_path(self.cache)), self.public_base))
                return

            prefix = f"/repos/{REPO}/releases/tags/"
            if path.startswith(prefix):
                tag = path[len(prefix) :]
                if "/" in tag or not tag:
                    self.send_error_text(HTTPStatus.BAD_REQUEST, "invalid tag")
                    return
                self.send_json(rewrite_release_urls(read_json(metadata_path(self.cache, tag)), self.public_base))
                return

            if path.startswith("/assets/"):
                filename = path[len("/assets/") :]
                asset = safe_asset_path(self.cache, filename)
                self.send_response(HTTPStatus.OK)
                self.send_header("Content-Type", "application/octet-stream")
                self.send_header("Content-Length", str(asset.stat().st_size))
                self.end_headers()
                with asset.open("rb") as f:
                    shutil.copyfileobj(f, self.wfile)
                return

            self.send_error_text(HTTPStatus.NOT_FOUND, "not found")
        except FileNotFoundError:
            self.send_error_text(HTTPStatus.NOT_FOUND, "not found")
        except RelayError as err:
            self.send_error_text(HTTPStatus.BAD_REQUEST, str(err))


def serve(cache: Path, port: int) -> None:
    if not latest_path(cache).is_file():
        raise RelayError(f"{latest_path(cache)} is missing; run --sync first")

    public_base = f"http://{HOST}:{port}"

    class Handler(RelayHandler):
        pass

    Handler.cache = cache
    Handler.public_base = public_base

    server = ThreadingHTTPServer((HOST, port), Handler)
    print(f"[serve] Serving cached artifacts on {public_base}")
    print("[serve] Configure Multica with:")
    print(f"        {update_env('MULTICA_UPDATE_GH_API_BASE', public_base)}")
    print(f"        {update_env('MULTICA_UPDATE_GH_DOWNLOAD_BASE', public_base + '/assets')}")
    server.serve_forever()


def update_env(name: str, value: str) -> str:
    return f"export {name}={value}"


def positive_port(value: str) -> int:
    port = int(value)
    if port <= 0 or port > 65535:
        raise argparse.ArgumentTypeError("port must be between 1 and 65535")
    return port


def parse_args(argv: Optional[Iterable[str]] = None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--cache", type=Path, default=DEFAULT_CACHE, help=f"cache directory (default: {DEFAULT_CACHE})")
    parser.add_argument("--port", type=positive_port, default=9876, help="localhost port for --serve")
    parser.add_argument("--tag", help="release tag to sync; default syncs latest")
    parser.add_argument("--sync", action="store_true", help="download and verify release artifacts")
    parser.add_argument("--serve", action="store_true", help="serve cached artifacts from 127.0.0.1")
    parser.add_argument("--all", action="store_true", help="run --sync then --serve")
    return parser.parse_args(argv)


def main(argv: Optional[Iterable[str]] = None) -> int:
    args = parse_args(argv)
    if args.all:
        args.sync = True
        args.serve = True
    if not args.sync and not args.serve:
        print("nothing to do; pass --sync, --serve, or --all", file=sys.stderr)
        return 2

    cache = args.cache.expanduser().resolve()
    try:
        if args.sync:
            sync_release(cache, args.tag)
        if args.serve:
            serve(cache, args.port)
    except (RelayError, urllib.error.URLError, OSError) as err:
        print(f"artifact-relay: {err}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
