#!/usr/bin/env python3
from __future__ import annotations

import json
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]


class WorkflowDeliveryTest(unittest.TestCase):
    def test_friday_tag_dispatches_the_binary_release(self) -> None:
        release = (ROOT / ".github/workflows/release.yml").read_text()
        friday = (ROOT / ".github/workflows/friday-release.yml").read_text()

        self.assertIn("  workflow_dispatch:\n", release)
        self.assertIn("  actions: write\n", friday)
        create = friday.index('gh release create "$TAG"')
        dispatch = friday.index('gh workflow run release.yml --ref "$TAG"')
        self.assertLess(create, dispatch)

    def test_firtal_release_has_no_cross_repo_write_token(self) -> None:
        release = (ROOT / ".github/workflows/release.yml").read_text()

        self.assertIn('echo "args=release --clean --skip=homebrew"', release)
        self.assertNotIn("${{ secrets.HOMEBREW_TAP_GITHUB_TOKEN }}", release)
        self.assertNotIn("\n  tap-mirror:\n", release)

    def test_desktop_publishes_to_the_private_source_release(self) -> None:
        release = (ROOT / ".github/workflows/release.yml").read_text()
        desktop_release_config = (
            ROOT / "apps/desktop/electron-builder.release.yml"
        ).read_text()

        desktop = release[release.index("\n  desktop:\n") :]
        self.assertIn("    needs: release\n", desktop)
        self.assertIn("GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}", desktop)
        self.assertIn("NODE_OPTIONS: --max-old-space-size=4096", desktop)
        self.assertIn("--config electron-builder.release.yml", desktop)
        self.assertNotIn("-c.publish.repo=firtal-cerebro", desktop)
        self.assertIn("extends: electron-builder.yml", desktop_release_config)
        self.assertIn("repo: firtal-cerebro", desktop_release_config)

    def test_linux_desktop_frees_output_between_architectures(self) -> None:
        release = (ROOT / ".github/workflows/release.yml").read_text()
        desktop = release[release.index("\n  desktop:\n") :]

        x64 = desktop.index("node scripts/package.mjs --linux --x64")
        cleanup = desktop.index("rm -rf dist")
        arm64 = desktop.index("node scripts/package.mjs --linux --arm64")

        self.assertLess(x64, cleanup)
        self.assertLess(cleanup, arm64)
        self.assertNotIn("--linux --x64 --arm64", desktop)
        self.assertIn("if: matrix.target != 'linux'", desktop)

    def test_desktop_avoids_monorepo_dependency_collection(self) -> None:
        package = json.loads((ROOT / "apps/desktop/package.json").read_text())

        self.assertNotIn("packageManager", package)


if __name__ == "__main__":
    unittest.main()
