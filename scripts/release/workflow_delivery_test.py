#!/usr/bin/env python3
from __future__ import annotations

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


if __name__ == "__main__":
    unittest.main()
