# Lazy inline image measurements

Measured on 2026-11-06, Linux x86_64, Intel Xeon Platinum 8259CL, Node v24.21.0, Chromium 145.0.7632.6. Production builds, 1280×860 CSS viewport, DPR 2, 4× CPU throttle during typing, 172 characters at 25ms inter-key delay. Median of three runs per cell (27 samples total); the no-image control runs on head with identical comment IDs, prose, timestamps and parent relationships.

Base: `61e7e8f966f53478f3efe68ec50c5a5fdd00a51f`. Measured head: `3426e3cc1e18daa507492c362b6c7869b4bfdb77`. Later commits only add these results. Both builds used byte-identical, checked-in real browser screenshots (2560×1720 and 2600×2160). The thread has 46 unique image URLs and the existing UI mounts 49 image elements, including three repeated thumbnails. Every after-scroll sample confirmed all images loaded before typing. All head samples had zero collapsed image boxes before typing.

Key latency is browser keydown timestamp to rAF plus a timer (a post-paint proxy), not hardware input latency. Long tasks cover typing through final image settlement. Layout-shift sum includes recent-input shifts; standard CLS excludes them and uses session windows. CPU scheduling and frame phase introduce noise; this is a shared development host, not a dedicated performance lab. The range column and raw samples make that variance visible. Results cover same-origin image URLs, not authenticated blob fallback.

| Scenario | Build | Typing ms | Long tasks: count / total ms | Keys >16.7ms: median (range) | Key p95 ms | Shift sum / CLS | Image bytes fetched |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| in-viewport | Base | 6275 | 4 / 486 | 38 (37–41) | 177.1 | 0.38 / 0 | 11641128 |
| in-viewport | Head | 6083 | 2 / 293 | 38 (32–38) | 23.6 | 0 / 0 | 2530680 |
| in-viewport | No-image control | 5884 | 1 / 169 | 29 (28–35) | 22 | 0 / 0 | 0 |
| after-scroll | Base | 5897 | 1 / 213 | 36 (32–37) | 22.9 | 0.38 / 0.38 | 11641128 |
| after-scroll | Head | 6067 | 1 / 236 | 52 (36–93) | 25.4 | 0 / 0 | 12445842 |
| after-scroll | No-image control | 6083 | 1 / 177 | 42 (41–81) | 23.8 | 0 / 0 | 0 |
| without-dimensions | Base | 6314 | 4 / 412 | 34 (32–40) | 146.7 | 0.222 / 0 | 11641128 |
| without-dimensions | Head | 5953 | 2 / 271 | 36 (34–43) | 23.9 | 0.023 / 0 | 3036816 |
| without-dimensions | No-image control | 5840 | 1 / 168 | 30 (29–38) | 22.7 | 0 / 0 | 0 |

See `comparison.json` for all raw key latencies, long tasks, hashes, build SHAs and timings.

Loading-time tail latency improved: in-viewport key p95 fell from 177.1 to 23.6 ms (control 22.0 ms), and historical-image p95 from 146.7 to 23.9 ms (control 22.7 ms). In-viewport long tasks fell from 4 / 486 ms to 2 / 293 ms, and served image bytes fell by 78.3% during that window. The count above one frame did **not** consistently improve (38→38 and 34→36), so this is evidence of shorter stalls, not elimination of slow keystrokes.

After scrolling through all images, head did not beat base: typing took 6067 versus 5897 ms, with the no-image control at 6083 ms. Over-frame counts varied widely on both head and control. There is no stable post-scroll typing win in these samples, and all images remain mounted. Historical-image layout shift fell from 0.222 to 0.023 but was not eliminated; zero CLS during typing must not be read as zero movement.

HTTP is mocked locally in fresh browser contexts; byte counts are harness-served bytes, including repeated URLs, not a network throughput benchmark.
