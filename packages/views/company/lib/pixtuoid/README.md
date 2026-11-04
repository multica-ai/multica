# pixtuoid company (vendored)

The company on the Company page is the pixtuoid engine, compiled to WebAssembly.

- Source: https://github.com/pixtuoid/pixtuoid at commit `b74dfb6`, crate `pixtuoid-web`, MIT (see `LICENSE`).
- `office.worker.ts` runs the engine in a worker and paints the canvas it owns; `office-protocol.ts` holds the messages.
- `wasm/` holds the built module and its wasm-bindgen glue (`wasm-bindgen` 0.2.129).
- `patch/pixtuoid-web-issues.patch` adds the issue API to `crates/pixtuoid-web/src/lib.rs`:
  `clear_script`, `issue_start`, `issue_present`, `issue_working`, `issue_end`, the issue `key` on each overlay label, and a walk-in dated 60 s before the first frame.
- The same patch exports the aquarium's position and every whiteboard's box in `overlay_json` (the page lays a button over each whiteboard that opens the kanban board), turns the studio's meeting room into the product company (a boss's desk, two staff desks with laptops and a small guest sofa, from the `boss_desk`, `staff_desk` and `reception_sofa` sprites), lays the studio floor out as a 3x2 grid of six desk rooms (numbered west to east, top row first) with ten desks each, a 5x2 pod per room (60 desks), and seats a worker in a chosen room (`issue_start`); `issue_present` tells when a worker has left the floor.
- The studio needs a floor of at least 416 by 171 layout units (`STUDIO_MIN_W/H` in the patch; `MIN_UNITS_W/H` in `office-protocol.ts` must match). A worker's figure is 12 units tall. The room, pod and desk constants (`COMPANY_ROOMS`, `COMPANY_ROOM_DESKS`, `COMPANY_DESKS` = 6 x 10 + 1, `STUDIO_*`) keep rooms and desks on the 4-unit routing grid, so every desk stays reachable; the patch adds a test that checks it.
- The web shim holds one still daytime sky under fog (`set_daylight("fog")`, the `Daylight` weather policy): no night, no moon, no lightning, no weather cycle. Its window views repaint only when the wall or the density changes, because at rest no cloud drifts and no glass moves. The native TUI keeps the clock's weather.
- The same patch exports the changed rects of the last written frame (`dirty_full`, `dirty_ptr`, `dirty_len`). `step` expands only those rects into its RGBA buffer, and `office.worker.ts` puts only those rects on the canvas. A frame that changed everywhere, or a resized canvas, still repaints whole.
- The same patch leaves the page's canvas alone on a held frame (the screen's flash hold keeps the last picture): no rects and no full repaint, and the next written frame repaints whole.
- The same patch hashes a window's glass one cell per word, not one `Option<Rgb>` at a time. A window is fingerprinted every frame, and this cut that cost by about four fifths. The fingerprint only tells frames apart, so the drawn frame does not change.
- The same patch resolves a window's sky row by row: the dither band and seam of a row are worked out once, each cell then takes one table lookup, and a run of equal cells blazes once (`nearest_split` in `dither.rs` is the row form of `nearest`, tested cell by cell). The frames are identical to the per-cell form, and the disc's own pane still resolves each cell in full.
- The same patch raises the cutaway's repaint pass cap from 8 to 70 (`MAX_PASSES` in `cutaway/canvas.rs`). Fewer merged boxes repaint fewer pixels, and each frame stays identical.
- The same patch sets `wanders: false` in `pixtuoid-scene/src/sim/mod.rs`, so idle workers stay at their desks.

To rebuild: check out the commit, apply the patch, then run
`cargo build -p pixtuoid-web --target wasm32-unknown-unknown --release` and
`wasm-bindgen --target web --out-dir <wasm dir> target/wasm32-unknown-unknown/release/pixtuoid_web.wasm`.
