#!/usr/bin/env python3
"""Transfer a release's Compose images without registry access on the target."""

import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile


COMPOSE = Path(__file__).resolve().parents[1] / "docker-compose.selfhost.yml"
DIGEST = re.compile(r"sha256:[0-9a-f]{64}")
PLATFORMS = ("linux/amd64", "linux/arm64")


def digest(stream):
    checksum = hashlib.sha256()
    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
        checksum.update(chunk)
    return "sha256:" + checksum.hexdigest()


def compose_images(release):
    # Resolve only image names. The operator's .env can contain credentials
    # and registry overrides; this bundle targets the official Compose stack.
    result = subprocess.check_output(
        ["docker", "compose", "--env-file", os.devnull, "-f", str(COMPOSE),
         "config", "--images"],
        env={"PATH": os.environ["PATH"], "HOME": os.environ["HOME"],
             "MULTICA_IMAGE_TAG": release, "JWT_SECRET": "compose-config-only"},
        text=True,
    )
    images = sorted(set(result.splitlines()))
    for service in ("backend", "web"):
        if f"ghcr.io/multica-ai/multica-{service}:{release}" not in images:
            raise ValueError(f"Compose does not select the {service} image for {release}")
    return images


def inspect(platform, reference):
    result = subprocess.check_output(
        ["docker", "image", "inspect", "--platform", platform, reference], text=True)
    image, = json.loads(result)
    if f"{image['Os']}/{image['Architecture']}" != platform:
        raise ValueError(f"{reference} has the wrong platform")
    if not DIGEST.fullmatch(image["Id"]):
        raise ValueError(f"{reference} has an invalid image ID")
    return image


def tagged_configs(archive):
    result = {}
    entries = json.load(archive.extractfile("manifest.json"))
    for entry in entries:
        tags = entry.get("RepoTags") or []
        if not tags:
            continue
        config = archive.getmember(entry["Config"])
        if not config.isfile():
            raise ValueError("Docker archive does not contain a regular image config")
        with archive.extractfile(config) as source:
            config_id = digest(source)
        for tag in tags:
            name = tag.removeprefix("docker.io/")
            if name in result:
                raise ValueError(f"duplicate image tag in Docker archive: {name}")
            result[name] = config_id
    return result


def check_archive(stream, images):
    # A tag can move between pull and save. The classic image store identifies
    # an image by config digest; containerd can identify it by the platform
    # manifest digest instead. Bind both representations to the archived config.
    with tarfile.open(fileobj=stream, mode="r:") as archive:
        configs = tagged_configs(archive)
        if set(configs) != set(images):
            raise ValueError("Docker archive image names or IDs do not match the manifest")
        for ref, config_id in configs.items():
            image_id = images[ref]["image_id"]
            if config_id != image_id:
                with archive.extractfile("blobs/sha256/" + image_id[7:]) as source:
                    descriptor = source.read()
                if ("sha256:" + hashlib.sha256(descriptor).hexdigest() != image_id
                        or json.loads(descriptor)["config"]["digest"] != config_id):
                    raise ValueError(f"Docker archive does not match the pulled image for {ref}")
            images[ref]["config_digest"] = config_id


def verify_loaded(platform, ref, record):
    image_id = inspect(platform, ref)["Id"]
    if image_id in (record["image_id"], record["config_digest"]):
        return
    # A different image store can synthesize a different manifest on load.
    # In that case compare the immutable config (which includes layer diff IDs).
    with tempfile.TemporaryFile() as stream:
        subprocess.run(["docker", "image", "save", "--platform", platform, ref],
                       stdout=stream, check=True)
        stream.seek(0)
        with tarfile.open(fileobj=stream, mode="r:") as archive:
            if tagged_configs(archive) != {ref: record["config_digest"]}:
                raise ValueError(f"loaded image ID differs for {ref}. Stop before starting the stack.")


def export_images(args):
    if args.bundle.exists():
        raise ValueError(f"{args.bundle} already exists")
    images = {}
    # Stage beside the destination so only a complete bundle becomes visible.
    with tempfile.TemporaryDirectory(dir=args.bundle.parent) as staging:
        folder = Path(staging) / "bundle"
        folder.mkdir()
        for ref in compose_images(args.release):
            subprocess.run(["docker", "pull", "--platform", args.platform, ref], check=True)
            image = inspect(args.platform, ref)
            repository = ref.rsplit(":", 1)[0]
            digests = {value.split("@", 1)[1] for value in image["RepoDigests"]
                       if value.removeprefix("docker.io/").startswith(repository + "@")}
            if len(digests) != 1 or not DIGEST.fullmatch(next(iter(digests))):
                raise ValueError(f"{ref} does not have an unambiguous registry digest")
            images[ref] = {"registry_digest": digests.pop(), "image_id": image["Id"]}

        archive = folder / "images.tar"
        subprocess.run(["docker", "image", "save", "--platform", args.platform,
                        "-o", str(archive), *images], check=True)
        with archive.open("rb") as stream:
            check_archive(stream, images)
            stream.seek(0)
            archive_digest = digest(stream)
        manifest = folder / "manifest.json"
        manifest.write_text(json.dumps({
            "version": 1, "release": args.release, "platform": args.platform,
            "images": images, "archive_digest": archive_digest,
        }, indent=2, sort_keys=True) + "\n", encoding="utf-8")
        with manifest.open("rb") as stream:
            manifest_digest = digest(stream)
        folder.rename(args.bundle)
        print("Manifest digest:", manifest_digest)


def import_images(args):
    manifest = (args.bundle / "manifest.json").read_bytes()
    if "sha256:" + hashlib.sha256(manifest).hexdigest() != args.manifest_digest:
        raise ValueError("manifest digest mismatch")
    data = json.loads(manifest)
    if (data["version"], data["release"], data["platform"]) != (1, args.release, args.platform):
        raise ValueError("manifest version, release or platform mismatch")
    images = data["images"]
    if sorted(images) != compose_images(args.release):
        raise ValueError("manifest does not match the images selected by Compose")
    configured = subprocess.check_output(
        ["docker", "compose", "-f", str(COMPOSE), "config", "--images"], text=True)
    if set(configured.splitlines()) != set(images):
        raise ValueError("target Compose image settings do not match the bundle")
    for ref, record in images.items():
        if not all(DIGEST.fullmatch(record[key]) for key in ("registry_digest", "image_id", "config_digest")):
            raise ValueError(f"{ref} has invalid digest metadata")
    with (args.bundle / "images.tar").open("rb") as stream:
        if digest(stream) != data["archive_digest"]:
            raise ValueError("image archive digest mismatch")
        stream.seek(0)
        subprocess.run(["docker", "image", "load", "--platform", args.platform],
                       stdin=stream, check=True)
    for ref, record in images.items():
        verify_loaded(args.platform, ref, record)
    print(f"Verified {len(images)} images for {args.release} on {args.platform}.")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest="action", required=True)
    for action in ("export", "import"):
        command = commands.add_parser(action)
        command.add_argument("release", help="exact release tag, for example v0.8.1")
        command.add_argument("platform", choices=PLATFORMS)
        command.add_argument("bundle", type=Path, help="bundle directory")
        if action == "import":
            command.add_argument("manifest_digest", help="sha256:... printed by export")
    args = parser.parse_args()
    if not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?", args.release):
        parser.error("supply an exact release tag")
    try:
        if args.action == "export":
            export_images(args)
        else:
            import_images(args)
    except (OSError, ValueError, KeyError, TypeError, subprocess.CalledProcessError, tarfile.TarError) as err:
        parser.exit(1, f"{parser.prog}: {err}\n")


if __name__ == "__main__":
    main()
