# pixtuoid office (vendored)

The office on the Office page is the pixtuoid engine, compiled to WebAssembly.

- Source: https://github.com/pixtuoid/pixtuoid at commit `b74dfb6`, crate `pixtuoid-web`, MIT (see `LICENSE`).
- `office.worker.ts` runs the engine in a worker and paints the canvas it owns; `office-protocol.ts` holds the messages.
- `wasm/` holds the built module and its wasm-bindgen glue (`wasm-bindgen` 0.2.129).
- `patch/pixtuoid-web-issues.patch` adds the issue API to `crates/pixtuoid-web/src/lib.rs`:
  `clear_script`, `issue_start`, `issue_present`, `issue_working`, `issue_end`, the issue `key` on each overlay label, and a walk-in dated 60 s before the first frame.
- The same patch exports the aquarium's position and every whiteboard's box in `overlay_json` (the page lays a button over each whiteboard that opens the kanban board), turns the studio's meeting room into the product office (a boss's desk, two staff desks with laptops and a small guest sofa, from the `boss_desk`, `staff_desk` and `reception_sofa` sprites), lays the studio floor out as a 3x2 grid of six desk rooms (numbered west to east, top row first) with ten desks each, a 5x2 pod per room (60 desks), and seats a worker in a chosen room (`issue_start`); `issue_present` tells when a worker has left the floor.
- The studio needs a floor of at least 416 by 171 layout units (`STUDIO_MIN_W/H` in the patch; `MIN_UNITS_W/H` in `office-protocol.ts` must match). A worker's figure is 12 units tall. The room, pod and desk constants (`OFFICE_ROOMS`, `OFFICE_ROOM_DESKS`, `OFFICE_DESKS` = 6 x 10 + 1, `STUDIO_*`) keep rooms and desks on the 4-unit routing grid, so every desk stays reachable; the patch adds a test that checks it.
- The web shim forces clear weather, so the window shows day or night by the clock.
- The same patch exports the changed rects of the last written frame (`dirty_full`, `dirty_ptr`, `dirty_len`). `step` expands only those rects into its RGBA buffer, and `office.worker.ts` puts only those rects on the canvas. A frame that changed everywhere, or a resized canvas, still repaints whole.
- The same patch hashes a window's glass one cell per word, not one `Option<Rgb>` at a time. A window is fingerprinted every frame, and this cut that cost by about four fifths. The fingerprint only tells frames apart, so the drawn frame does not change.
- The same patch raises the cutaway's repaint pass cap from 8 to 70 (`MAX_PASSES` in `cutaway/canvas.rs`). Fewer merged boxes repaint fewer pixels, and each frame stays identical.
- The same patch sets `wanders: false` in `pixtuoid-scene/src/sim/mod.rs`, so idle workers stay at their desks.

To rebuild: check out the commit, apply the patch, then run
`cargo build -p pixtuoid-web --target wasm32-unknown-unknown --release` and
`wasm-bindgen --target web --out-dir <wasm dir> target/wasm32-unknown-unknown/release/pixtuoid_web.wasm`.
