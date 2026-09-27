#!/usr/bin/env python3
"""Exercise the actual receiving-system reference snippet, without dependencies.

Run: python3 scripts/test-task-token-verifier.py
"""
import ast
from concurrent.futures import ThreadPoolExecutor
from pathlib import Path
import threading
from types import SimpleNamespace
import unittest
from unittest.mock import Mock


class JWKSReferenceTests(unittest.TestCase):
    def setUp(self):
        doc = (Path(__file__).resolve().parents[1] / "TASK_IDENTITY_TOKENS_AI.md").read_text()
        snippet = doc.split("```python\n", 1)[1].split("```", 1)[0]
        tree = ast.parse(snippet)
        tree.body = [node for node in tree.body if not isinstance(node, (ast.Import, ast.ImportFrom))]
        self.now = 10000.0
        self.get = Mock()
        self.env = {
            "threading": threading,
            "time": SimpleNamespace(monotonic=lambda: self.now),
            "requests": SimpleNamespace(get=self.get, RequestException=OSError),
        }
        exec(compile(tree, "TASK_IDENTITY_TOKENS_AI.md", "exec"), self.env)
        self.respond([{"kid": "old"}])

    def respond(self, keys):
        self.get.side_effect = None
        self.get.return_value = SimpleNamespace(raise_for_status=lambda: None, json=lambda: {"keys": keys})

    def lookup(self, kid):
        return self.env["_key_for"](kid)

    def test_warm_cache_and_unknown_keys_share_cooldown(self):
        for _ in range(20):
            self.assertEqual(self.lookup("old"), {"kid": "old"})
        for i in range(20):
            with self.assertRaises(ValueError):
                self.lookup("unknown-" + str(i))
        self.assertEqual(self.get.call_count, 1)
        self.now += 30
        for i in range(20):
            with self.assertRaises(ValueError):
                self.lookup("new-unknown-" + str(i))
        self.assertEqual(self.get.call_count, 2)

    def test_rotation_after_cooldown(self):
        self.lookup("old")
        self.respond([{"kid": "new"}])
        self.now += 30
        self.assertEqual(self.lookup("new"), {"kid": "new"})
        self.assertEqual(self.get.call_count, 2)
        with self.assertRaises(ValueError):
            self.lookup("old")

    def test_stale_outage_retries_once_per_cooldown(self):
        self.lookup("old")
        self.now += 300
        self.get.side_effect = OSError("offline")
        for _ in range(20):
            self.assertEqual(self.lookup("old"), {"kid": "old"})
        self.assertEqual(self.get.call_count, 2)
        self.now += 30
        self.lookup("old")
        self.assertEqual(self.get.call_count, 3)

    def test_cold_outage_also_observes_cooldown(self):
        self.get.side_effect = OSError("offline")
        for _ in range(20):
            with self.assertRaises(ValueError):
                self.lookup("old")
        self.assertEqual(self.get.call_count, 1)
        self.now += 30
        self.respond([{"kid": "old"}])
        self.lookup("old")
        self.assertEqual(self.get.call_count, 2)

    def test_maximum_stale_age_fails_closed(self):
        self.lookup("old")
        self.get.side_effect = OSError("offline")
        self.now += 3599
        self.lookup("old")
        self.now += 1
        with self.assertRaises(ValueError):
            self.lookup("old")

    def test_invalid_response_does_not_replace_good_cache(self):
        self.lookup("old")
        self.now += 300
        self.respond("invalid keys")
        self.assertEqual(self.lookup("old"), {"kid": "old"})
        self.assertEqual(self.get.call_count, 2)

    def test_http_error_does_not_replace_good_cache(self):
        self.lookup("old")
        self.now += 300
        self.get.return_value.raise_for_status = Mock(side_effect=OSError("503"))
        self.assertEqual(self.lookup("old"), {"kid": "old"})
        self.assertEqual(self.get.call_count, 2)

    def test_refresh_is_coalesced_and_warm_reads_do_not_block(self):
        self.lookup("old")
        self.now += 300
        entered, release = threading.Event(), threading.Event()
        response = self.get.return_value

        def slow_refresh(*args, **kwargs):
            entered.set()
            if not release.wait(5):
                raise OSError("test refresh timed out")
            return response

        self.get.side_effect = slow_refresh
        with ThreadPoolExecutor(max_workers=8) as pool:
            first = pool.submit(self.lookup, "old")
            self.assertTrue(entered.wait(2))
            try:
                futures = [pool.submit(self.lookup, "old") for _ in range(20)]
                for future in futures:
                    self.assertEqual(future.result(timeout=1), {"kid": "old"})
                self.assertEqual(self.get.call_count, 2)
            finally:
                release.set()
            self.assertEqual(first.result(timeout=2), {"kid": "old"})


if __name__ == "__main__":
    unittest.main()
