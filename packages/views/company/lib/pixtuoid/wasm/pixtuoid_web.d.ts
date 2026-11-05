/* tslint:disable */
/* eslint-disable */

export class Company {
    free(): void;
    [Symbol.dispose](): void;
    /**
     * Create the audio engine. Idempotent — a second call is ignored. Costs
     * nothing until `audio_warmup_step`.
     */
    audio_begin(): void;
    /**
     * The bus ceiling (dBFS) JS's limiter holds: [`pixtuoid_scene::audio::CEILING_DBFS`].
     */
    audio_ceiling_dbfs(): number;
    /**
     * The time constant JS's `setTargetAtTime` smooths each tick's gains
     * over, in seconds: `GAIN_DEZIPPER`.
     */
    audio_gain_dezipper_s(): number;
    /**
     * JS's limiter attack, in seconds: [`pixtuoid_scene::audio::LIMITER_ATTACK`].
     */
    audio_limiter_attack_s(): number;
    /**
     * JS's limiter release, in seconds: [`pixtuoid_scene::audio::LIMITER_RELEASE`].
     */
    audio_limiter_release_s(): number;
    /**
     * How many loop stems a tick's `gains` carry:
     * [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL)'s.
     */
    audio_loop_count(): number;
    audio_loop_len(idx: number): number;
    /**
     * Zero-copy pointer/length into the looping bed samples for stem `idx`, in
     * [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL) order.
     * RE-READ after warmup completes.
     */
    audio_loop_ptr(idx: number): number;
    /**
     * How far ahead the music worklet queues the worker's chunks, in seconds:
     * [`pixtuoid_scene::audio::MUSIC_AHEAD`].
     */
    audio_music_ahead_s(): number;
    audio_oneshot_len(pool: number, idx: number): number;
    /**
     * Zero-copy pointer/length into a one-shot buffer: `pool` is the wire index
     * [`OneShotPool::from_wire`] reads, `idx` the pool slot.
     */
    audio_oneshot_ptr(pool: number, idx: number): number;
    /**
     * Every one-shot pool's wire: JS discovers a pool's slots, but not the pools.
     */
    audio_oneshot_wires(): Uint8Array;
    /**
     * The engine's sample rate (Hz) — JS builds its `AudioBuffer`s at this rate
     * (the browser resamples to the AudioContext rate).
     */
    audio_sample_rate(): number;
    /**
     * Advance the audio one tick at `now_ms` (the site's pause-shifted clock,
     * same as `step`) and return the JS glue commands as JSON:
     * `{"gains":[g…],"plays":[[poolWire,idx,gain],…],"music":{"controls":[…],"gain":g}|null}`,
     * one gain per [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL)
     * stem — JS ramps each GainNode to its gain, spawns the one-shots, ramps
     * the music's GainNode and hands its controls, unread, to the music
     * worker (`null` until the first frame).
     */
    audio_tick(now_ms: number): string;
    /**
     * Build ONE synthesis piece; returns pieces REMAINING (0 = ready to upload
     * buffers + tick). The host pumps one piece per task, so no frame waits on
     * the whole synthesis.
     */
    audio_warmup_step(): number;
    /**
     * Drop the hero's scripted cast, so only the issues this office is given show.
     */
    clear_script(): void;
    /**
     * Whether the last written frame repainted everywhere; when false, only the rects at `dirty_ptr` changed.
     */
    dirty_full(): boolean;
    /**
     * The number of `u32`s at `dirty_ptr`.
     */
    dirty_len(): number;
    /**
     * Pointer to the changed rects of the last written frame: `dirty_len` `u32`s, four per rect (`x, y, w, h`).
     */
    dirty_ptr(): number;
    /**
     * Byte length of the RGBA frame: `w`×`h` units × [`Company::scale`]² × 4.
     */
    frame_len(): number;
    /**
     * Pointer to the RGBA frame in wasm linear memory ([`Company::frame_len`]
     * bytes).
     *
     * CONTRACT: re-read this (and rebuild any `Uint8ClampedArray` view) after
     * EVERY `step` — a canvas resize reallocates the staging buffer, and any
     * wasm `memory.grow` invalidates existing JS views into linear memory even
     * when the pointer value is unchanged.
     */
    frame_ptr(): number;
    /**
     * Hire one more agent: a new coworker walks into the background office,
     * works a few spells, and heads out `script::HIRE_STAY_MS` later. Refused
     * (`false`) before the first `step` (no clock yet), while `MAX_LIVE` hires
     * are already alive, and when the canvas-sized office has no free desk.
     * Never throws.
     */
    hire(): boolean;
    /**
     * Whether the office's sky shows the SUN at hour-of-day `hour` (0..24) — the
     * site's sky-slider thumb reads this, so it delegates to the engine's ONE
     * day/night boundary rather than restating it.
     */
    is_day(hour: number): boolean;
    /**
     * The worker for `key` leaves its desk.
     */
    issue_end(key: string): void;
    /**
     * Whether a worker for `key` is still in the office: a worker that has
     * been ended stays until it has walked out of the door.
     */
    issue_present(key: string): boolean;
    /**
     * Seat a worker for the issue `key` at a free desk of office room `room`
     * (0..[`OFFICE_ROOMS`], or [`BOSS_ROOM`] for the boss's desk). `seated` puts it at its desk from the first frame;
     * otherwise it walks in from the door. `false` before the first `step`,
     * when that room has no desk left, or when the office has none left.
     */
    issue_start(key: string, room: number, seated: boolean): boolean;
    /**
     * The worker for `key` works (`true`) or sits idle (`false`).
     */
    issue_working(key: string, working: boolean): void;
    /**
     * Build an office seeded with `seed` (drives the layout variant).
     *
     * # Errors
     *
     * If the bundled sprite pack fails to parse.
     */
    constructor(seed: number);
    /**
     * Export the current frame's name-badge labels + neon wall-board TEXT as a
     * small JSON string for the site's DOM overlay.
     *
     * The wasm office renders at a SMALL buffer that CSS upscales with
     * `image-rendering: pixelated`, so anti-aliased text CANNOT be baked into the
     * pixels — the site lays crisp DOM spans over the canvas from this model
     * instead. Coordinates are the LAYOUT units `step` takes (a label's `x` is the sprite
     * CENTER, `y` its head-top; the board `rect` is the neon-panel interior).
     * Colors are RESOLVED against the CURRENT theme. Call right after `step` (it
     * reads the step's clock).
     */
    overlay_json(): string;
    /**
     * Frame pixels per layout unit `step` takes.
     */
    scale(): number;
    /**
     * Hold the weather `name` names at midday for good: no night, no weather
     * cycle, no lightning, and the outside stands still, so its views are
     * drawn once. An unrecognized name keeps the policy as it was.
     */
    set_daylight(name: string): void;
    /**
     * Recolor the whole office to one of the [`ALL_THEMES`](pixtuoid_scene::theme::ALL_THEMES) by name. Unknown
     * name = no-op.
     */
    set_theme(name: string): void;
    /**
     * Force the weather [`WeatherPolicy::from_name`] names, or `None` (or an
     * unrecognized name) to follow the clock-based cycle.
     */
    set_weather(name?: string | null): void;
    /**
     * Advance to `now_ms` and render an office `w`×`h` layout units big into
     * the RGBA staging buffer, [`Company::scale`] pixels per unit. While the
     * flash hold keeps this frame back, the buffer keeps the last one.
     *
     * CONTRACT: `now_ms` must be UNIX-epoch milliseconds — `Date.now()`, NOT
     * `performance.now()` and NOT a `requestAnimationFrame` timestamp: those are
     * ms-since-page-load, which pins the day/night cycle and wall clock at 1970.
     */
    step(now_ms: number, w: number, h: number): void;
}

export type InitInput = RequestInfo | URL | Response | BufferSource | WebAssembly.Module;

export interface InitOutput {
    readonly memory: WebAssembly.Memory;
    readonly __wbg_company_free: (a: number, b: number) => void;
    readonly company_audio_begin: (a: number) => void;
    readonly company_audio_ceiling_dbfs: (a: number) => number;
    readonly company_audio_gain_dezipper_s: (a: number) => number;
    readonly company_audio_limiter_attack_s: (a: number) => number;
    readonly company_audio_limiter_release_s: (a: number) => number;
    readonly company_audio_loop_count: (a: number) => number;
    readonly company_audio_loop_len: (a: number, b: number) => number;
    readonly company_audio_loop_ptr: (a: number, b: number) => number;
    readonly company_audio_music_ahead_s: (a: number) => number;
    readonly company_audio_oneshot_len: (a: number, b: number, c: number) => number;
    readonly company_audio_oneshot_ptr: (a: number, b: number, c: number) => number;
    readonly company_audio_oneshot_wires: (a: number) => [number, number];
    readonly company_audio_sample_rate: (a: number) => number;
    readonly company_audio_tick: (a: number, b: number) => [number, number];
    readonly company_audio_warmup_step: (a: number) => number;
    readonly company_clear_script: (a: number) => void;
    readonly company_dirty_full: (a: number) => number;
    readonly company_dirty_len: (a: number) => number;
    readonly company_dirty_ptr: (a: number) => number;
    readonly company_frame_len: (a: number) => number;
    readonly company_frame_ptr: (a: number) => number;
    readonly company_hire: (a: number) => number;
    readonly company_is_day: (a: number, b: number) => number;
    readonly company_issue_end: (a: number, b: number, c: number) => void;
    readonly company_issue_present: (a: number, b: number, c: number) => number;
    readonly company_issue_start: (a: number, b: number, c: number, d: number, e: number) => number;
    readonly company_issue_working: (a: number, b: number, c: number, d: number) => void;
    readonly company_new: (a: number) => [number, number, number];
    readonly company_overlay_json: (a: number) => [number, number];
    readonly company_scale: (a: number) => number;
    readonly company_set_daylight: (a: number, b: number, c: number) => void;
    readonly company_set_theme: (a: number, b: number, c: number) => void;
    readonly company_set_weather: (a: number, b: number, c: number) => void;
    readonly company_step: (a: number, b: number, c: number, d: number) => void;
    readonly __wbindgen_externrefs: WebAssembly.Table;
    readonly __wbindgen_free: (a: number, b: number, c: number) => void;
    readonly __wbindgen_malloc: (a: number, b: number) => number;
    readonly __wbindgen_realloc: (a: number, b: number, c: number, d: number) => number;
    readonly __externref_table_dealloc: (a: number) => void;
    readonly __wbindgen_start: () => void;
}

export type SyncInitInput = BufferSource | WebAssembly.Module;

/**
 * Instantiates the given `module`, which can either be bytes or
 * a precompiled `WebAssembly.Module`.
 *
 * @param {{ module: SyncInitInput }} module - Passing `SyncInitInput` directly is deprecated.
 *
 * @returns {InitOutput}
 */
export function initSync(module: { module: SyncInitInput } | SyncInitInput): InitOutput;

/**
 * If `module_or_path` is {RequestInfo} or {URL}, makes a request and
 * for everything else, calls `WebAssembly.instantiate` directly.
 *
 * @param {{ module_or_path: InitInput | Promise<InitInput> }} module_or_path - Passing `InitInput` directly is deprecated.
 *
 * @returns {Promise<InitOutput>}
 */
export default function __wbg_init (module_or_path?: { module_or_path: InitInput | Promise<InitInput> } | InitInput | Promise<InitInput>): Promise<InitOutput>;
