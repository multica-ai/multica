# pixtuoid office (vendored)

The office on the Office page is the pixtuoid engine, compiled to WebAssembly.

- Source: https://github.com/pixtuoid/pixtuoid at commit `b74dfb6`, crate `pixtuoid-web`, MIT (see `LICENSE`).
- `office.worker.ts` runs the engine in a worker and paints the canvas it owns; `office-protocol.ts` holds the messages.
- `wasm/` holds the built module and its wasm-bindgen glue (`wasm-bindgen` 0.2.129).
- `patch/pixtuoid-web-issues.patch` adds the issue API to `crates/pixtuoid-web/src/lib.rs`:
  `clear_script`, `issue_start`, `issue_present`, `issue_working`, `issue_end`, the issue `key` on each overlay label, and a walk-in dated 60 s before the first frame.
- The same patch exports the aquarium's position and every whiteboard's box in `overlay_json` (the page lays a button over each whiteboard that opens the kanban board), turns the studio's meeting room into the product office (a boss's desk, two staff desks with laptops and a small guest sofa, from the `boss_desk`, `staff_desk` and `reception_sofa` sprites), lays the studio floor out as a 2x2 grid of four desk rooms with four desks each, and seats a worker in a chosen room (`issue_start`); `issue_present` tells when a worker has left the floor.
- The web shim forces clear weather, so the window shows day or night by the clock.
- The same patch sets `wanders: false` in `pixtuoid-scene/src/sim/mod.rs`, so idle workers stay at their desks.

To rebuild: check out the commit, apply the patch, then run
`cargo build -p pixtuoid-web --target wasm32-unknown-unknown --release` and
`wasm-bindgen --target web --out-dir <wasm dir> target/wasm32-unknown-unknown/release/pixtuoid_web.wasm`.
