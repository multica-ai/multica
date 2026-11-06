# Screenshot fixtures

These are actual PNG screenshots of the Multica production web UI, captured by
Chromium 145.0.7632.6 on 2026-11-06 at deviceScaleFactor=2. They contain only the
synthetic `Perf` workspace from `image-heavy-thread.spec.ts`, with no account data.
They are not procedurally painted or AI-generated images.

- `issue-2560x1720.png`: 1280×860 CSS viewport, issue header and comment thread.
- `thread-2600x2160.png`: 1300×1080 CSS viewport, scrolled to the root comment.

Capture source: upstream `61e7e8f966f53478f3efe68ec50c5a5fdd00a51f` plus the
lazy-image implementation in this PR. Capture mode omits image attachments to
avoid recursive screenshots. The measured builds both receive these exact bytes
at 46 distinct URLs. The existing UI also mounts three repeated thumbnails;
the benchmark records DOM image count separately from unique image URLs.

To regenerate intentionally, start a production build, then run:

```bash
PLAYWRIGHT_BASE_URL=http://localhost:3000 PERF_SCENARIO=image-thread PERF_CAPTURE_SCREENSHOTS=1 pnpm exec playwright test --config=playwright.perf.config.ts image-heavy-thread.spec.ts
```

Commit both PNGs together and bump `FIXTURE_VERSION`. Normal measurements never
recapture the fixture. Their reports hash the complete fixture and PNG bytes.
