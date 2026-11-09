import hashlib
import importlib.util
import io
import json
from pathlib import Path
import subprocess
import tarfile
import tempfile
import unittest
from types import SimpleNamespace
from unittest.mock import patch

spec = importlib.util.spec_from_file_location(
    "selfhost_images", Path(__file__).with_name("selfhost-images.py"))
transfer = importlib.util.module_from_spec(spec)
spec.loader.exec_module(transfer)


class TransferTest(unittest.TestCase):
    def setUp(self):
        tmp = tempfile.TemporaryDirectory()
        self.addCleanup(tmp.cleanup)
        self.args = SimpleNamespace(release="v0.8.1", platform="linux/amd64",
                                    bundle=Path(tmp.name) / "export")
        self.refs = ["ghcr.io/multica-ai/multica-backend:v0.8.1",
                     "ghcr.io/multica-ai/multica-web:v0.8.1", "pgvector/pgvector:pg17"]
        self.target_refs = self.refs[:]
        self.config = {ref: json.dumps({"test-ref": ref}).encode() for ref in self.refs}
        self.loaded = []
        self.save_refs = self.refs[:]
        self.missing_platform = False
        self.bad_image = False
        self.oci = False
        for name in ("run", "check_output"):
            mock = patch.object(transfer.subprocess, name, side_effect=getattr(self, name + "_docker"))
            mock.start()
            self.addCleanup(mock.stop)

    def image_id(self, ref):
        return "sha256:" + hashlib.sha256(self.config[ref]).hexdigest()

    def check_output_docker(self, cmd, **kwargs):
        if "compose" in cmd:
            self.assertEqual(cmd[-2:], ["config", "--images"])
            return "\n".join(self.refs if "--env-file" in cmd else self.target_refs)
        self.assertEqual(cmd[:5], ["docker", "image", "inspect", "--platform", "linux/amd64"])
        ref = cmd[-1]
        image_id = self.image_id(ref)
        if self.oci:
            descriptor = json.dumps({"config": {"digest": image_id}}).encode()
            image_id = "sha256:" + hashlib.sha256(descriptor).hexdigest()
        return json.dumps([{"Os": "linux", "Architecture": "amd64",
                            "Id": image_id if not self.bad_image else "sha256:" + "1" * 64,
                            "RepoDigests": [ref.rsplit(":", 1)[0] + "@sha256:" + "a" * 64]}])

    def run_docker(self, cmd, **kwargs):
        self.assertTrue(kwargs["check"])
        if cmd[1] == "pull":
            self.assertFalse(self.loaded)
            if self.missing_platform:
                raise subprocess.CalledProcessError(1, cmd)
            return
        if cmd[2] == "load":
            self.assertEqual(cmd, ["docker", "image", "load", "--platform", self.args.platform])
            self.loaded.append(kwargs["stdin"].read())
            return
        self.assertEqual(cmd[:5], ["docker", "image", "save", "--platform", self.args.platform])
        if "-o" in cmd:
            archive = tarfile.open(cmd[cmd.index("-o") + 1], "w")
            refs = self.save_refs
        else:
            archive = tarfile.open(fileobj=kwargs["stdout"], mode="w")
            refs = [cmd[-1]]
        with archive:
            def add(name, value):
                member = tarfile.TarInfo(name)
                member.size = len(value)
                archive.addfile(member, io.BytesIO(value))
            entries = []
            for ref in refs:
                config_name = "blobs/sha256/" + self.image_id(ref).split(":")[1]
                add(config_name, b"wrong" if self.bad_image else self.config[ref])
                if self.oci:
                    descriptor = json.dumps({"config": {"digest": self.image_id(ref)}}).encode()
                    add("blobs/sha256/" + hashlib.sha256(descriptor).hexdigest(), descriptor)
                entries.append({"Config": config_name, "RepoTags": [ref], "Layers": []})
            add("manifest.json", json.dumps(entries).encode())

    def export(self):
        transfer.export_images(self.args)
        with (self.args.bundle / "manifest.json").open("rb") as stream:
            self.args.manifest_digest = transfer.digest(stream)

    def test_roundtrip_and_loaded_identity(self):
        self.export()
        transfer.import_images(self.args)
        self.assertEqual(self.loaded, [(self.args.bundle / "images.tar").read_bytes()])
        self.bad_image = True
        with self.assertRaisesRegex(ValueError, "loaded image ID differs"):
            transfer.import_images(self.args)

    def test_containerd_manifest_id_and_classic_config_id(self):
        self.oci = True
        self.export()
        self.oci = False
        transfer.import_images(self.args)
        self.assertEqual(len(self.loaded), 1)

    def test_classic_export_loaded_with_a_new_containerd_manifest(self):
        self.export()
        self.oci = True
        transfer.import_images(self.args)
        self.assertEqual(len(self.loaded), 1)

    def test_missing_platform_or_incomplete_archive_never_publishes_bundle(self):
        self.missing_platform = True
        with self.assertRaises(subprocess.CalledProcessError):
            self.export()
        self.assertFalse(self.args.bundle.exists())
        self.missing_platform = False
        self.save_refs.pop()
        with self.assertRaisesRegex(ValueError, "archive image names or IDs"):
            self.export()
        self.assertFalse(self.args.bundle.exists())

    def test_wrong_release_platform_manifest_or_checksum_never_loads(self):
        self.export()
        for field, value in (("release", "v0.3.0"), ("platform", "linux/arm64"),
                             ("manifest_digest", "sha256:" + "0" * 64)):
            with self.subTest(field=field), patch.object(self.args, field, value):
                with self.assertRaisesRegex(ValueError, "mismatch"):
                    transfer.import_images(self.args)
        self.target_refs[0] = self.target_refs[0].replace("v0.8.1", "latest")
        with self.assertRaisesRegex(ValueError, "target Compose image settings"):
            transfer.import_images(self.args)
        self.target_refs = self.refs[:]
        (self.args.bundle / "images.tar").write_bytes(b"damaged")
        with self.assertRaisesRegex(ValueError, "archive digest mismatch"):
            transfer.import_images(self.args)
        self.assertEqual(self.loaded, [])


if __name__ == "__main__":
    unittest.main()
