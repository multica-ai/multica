#!/usr/bin/env python3
from __future__ import annotations

import importlib.util
import tempfile
import unittest
from pathlib import Path

MODULE_PATH = Path(__file__).with_name("render_docs_release_notes.py")
spec = importlib.util.spec_from_file_location("render_docs_release_notes", MODULE_PATH)
assert spec is not None
renderer = importlib.util.module_from_spec(spec)
assert spec.loader is not None
spec.loader.exec_module(renderer)


class RenderDocsReleaseNotesTest(unittest.TestCase):
    def test_renders_mdx_and_prepends_index_entry(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            notes = root / "release-notes.md"
            notes.write_text(
                "\n".join(
                    [
                        "## v1.0.0",
                        "",
                        "_Changes since [v0.6.0](https://example.test) - bump: **major**_",
                        "",
                        "### New",
                        "- Added public release dashboard [#12](https://example.test/12)",
                        "",
                        "### Fixed",
                        "- Fixed docs links",
                        "",
                    ]
                )
                + "\n"
            )
            notes_dir = root / "release-notes"
            index = notes_dir / "index.mdx"
            index.parent.mkdir()
            index.write_text(
                "\n".join(
                    [
                        "---",
                        "title: Hvad er nyt",
                        "description: Oversigt over Multica-versioner - nyeste foerst.",
                        "---",
                        "",
                        "Intro.",
                        "",
                        "- [**v0.5.0** - 8. maj 2026](/docs/cerebro/release-notes/v0.5.0) - Old.",
                        "",
                    ]
                )
            )

            output = renderer.render_docs_release_notes(
                notes_path=notes,
                notes_dir=notes_dir,
                index_path=index,
                tag="v1.0.0",
                date="2026-05-22",
            )

            self.assertEqual(output, notes_dir / "v1.0.0.mdx")
            mdx = output.read_text()
            self.assertIn('title: "Multica v1.0.0"', mdx)
            self.assertIn("date: 2026-05-22", mdx)
            self.assertIn("## New", mdx)
            self.assertIn("- Added public release dashboard", mdx)

            index_lines = index.read_text().splitlines()
            self.assertIn(
                "- [**v1.0.0** - 22. maj 2026](/docs/cerebro/release-notes/v1.0.0) - 2 release bullets: 1 new, 1 fixed.",
                index_lines,
            )
            self.assertLess(
                index_lines.index(
                    "- [**v1.0.0** - 22. maj 2026](/docs/cerebro/release-notes/v1.0.0) - 2 release bullets: 1 new, 1 fixed."
                ),
                index_lines.index(
                    "- [**v0.5.0** - 8. maj 2026](/docs/cerebro/release-notes/v0.5.0) - Old."
                ),
            )

    def test_replaces_existing_index_entry_for_same_tag(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            notes = root / "release-notes.md"
            notes.write_text("## v1.0.0\n\n### Fixed\n- Fixed docs links\n")
            notes_dir = root / "release-notes"
            index = notes_dir / "index.mdx"
            index.parent.mkdir()
            index.write_text(
                "- [**v1.0.0** - 1. maj 2026](/docs/cerebro/release-notes/v1.0.0) - stale.\n"
            )

            renderer.render_docs_release_notes(
                notes_path=notes,
                notes_dir=notes_dir,
                index_path=index,
                tag="v1.0.0",
                date="2026-05-22",
            )

            entries = [line for line in index.read_text().splitlines() if "**v1.0.0**" in line]
            self.assertEqual(len(entries), 1)
            self.assertIn("22. maj 2026", entries[0])


if __name__ == "__main__":
    unittest.main()
