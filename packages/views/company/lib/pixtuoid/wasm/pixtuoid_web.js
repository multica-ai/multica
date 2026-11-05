/* @ts-self-types="./pixtuoid_web.d.ts" */

export class Company {
    __destroy_into_raw() {
        const ptr = this.__wbg_ptr;
        this.__wbg_ptr = 0;
        CompanyFinalization.unregister(this);
        return ptr;
    }
    free() {
        const ptr = this.__destroy_into_raw();
        wasm.__wbg_company_free(ptr, 0);
    }
    /**
     * Create the audio engine. Idempotent — a second call is ignored. Costs
     * nothing until `audio_warmup_step`.
     */
    audio_begin() {
        wasm.company_audio_begin(this.__wbg_ptr);
    }
    /**
     * The bus ceiling (dBFS) JS's limiter holds: [`pixtuoid_scene::audio::CEILING_DBFS`].
     * @returns {number}
     */
    audio_ceiling_dbfs() {
        const ret = wasm.company_audio_ceiling_dbfs(this.__wbg_ptr);
        return ret;
    }
    /**
     * The time constant JS's `setTargetAtTime` smooths each tick's gains
     * over, in seconds: `GAIN_DEZIPPER`.
     * @returns {number}
     */
    audio_gain_dezipper_s() {
        const ret = wasm.company_audio_gain_dezipper_s(this.__wbg_ptr);
        return ret;
    }
    /**
     * JS's limiter attack, in seconds: [`pixtuoid_scene::audio::LIMITER_ATTACK`].
     * @returns {number}
     */
    audio_limiter_attack_s() {
        const ret = wasm.company_audio_limiter_attack_s(this.__wbg_ptr);
        return ret;
    }
    /**
     * JS's limiter release, in seconds: [`pixtuoid_scene::audio::LIMITER_RELEASE`].
     * @returns {number}
     */
    audio_limiter_release_s() {
        const ret = wasm.company_audio_limiter_release_s(this.__wbg_ptr);
        return ret;
    }
    /**
     * How many loop stems a tick's `gains` carry:
     * [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL)'s.
     * @returns {number}
     */
    audio_loop_count() {
        const ret = wasm.company_audio_loop_count(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * @param {number} idx
     * @returns {number}
     */
    audio_loop_len(idx) {
        const ret = wasm.company_audio_loop_len(this.__wbg_ptr, idx);
        return ret >>> 0;
    }
    /**
     * Zero-copy pointer/length into the looping bed samples for stem `idx`, in
     * [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL) order.
     * RE-READ after warmup completes.
     * @param {number} idx
     * @returns {number}
     */
    audio_loop_ptr(idx) {
        const ret = wasm.company_audio_loop_ptr(this.__wbg_ptr, idx);
        return ret >>> 0;
    }
    /**
     * How far ahead the music worklet queues the worker's chunks, in seconds:
     * [`pixtuoid_scene::audio::MUSIC_AHEAD`].
     * @returns {number}
     */
    audio_music_ahead_s() {
        const ret = wasm.company_audio_music_ahead_s(this.__wbg_ptr);
        return ret;
    }
    /**
     * @param {number} pool
     * @param {number} idx
     * @returns {number}
     */
    audio_oneshot_len(pool, idx) {
        const ret = wasm.company_audio_oneshot_len(this.__wbg_ptr, pool, idx);
        return ret >>> 0;
    }
    /**
     * Zero-copy pointer/length into a one-shot buffer: `pool` is the wire index
     * [`OneShotPool::from_wire`] reads, `idx` the pool slot.
     * @param {number} pool
     * @param {number} idx
     * @returns {number}
     */
    audio_oneshot_ptr(pool, idx) {
        const ret = wasm.company_audio_oneshot_ptr(this.__wbg_ptr, pool, idx);
        return ret >>> 0;
    }
    /**
     * Every one-shot pool's wire: JS discovers a pool's slots, but not the pools.
     * @returns {Uint8Array}
     */
    audio_oneshot_wires() {
        const ret = wasm.company_audio_oneshot_wires(this.__wbg_ptr);
        var v1 = getArrayU8FromWasm0(ret[0], ret[1]).slice();
        wasm.__wbindgen_free(ret[0], ret[1] * 1, 1);
        return v1;
    }
    /**
     * The engine's sample rate (Hz) — JS builds its `AudioBuffer`s at this rate
     * (the browser resamples to the AudioContext rate).
     * @returns {number}
     */
    audio_sample_rate() {
        const ret = wasm.company_audio_sample_rate(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Advance the audio one tick at `now_ms` (the site's pause-shifted clock,
     * same as `step`) and return the JS glue commands as JSON:
     * `{"gains":[g…],"plays":[[poolWire,idx,gain],…],"music":{"controls":[…],"gain":g}|null}`,
     * one gain per [`LoopStem::ALL`](pixtuoid_scene::audio::mixer::LoopStem::ALL)
     * stem — JS ramps each GainNode to its gain, spawns the one-shots, ramps
     * the music's GainNode and hands its controls, unread, to the music
     * worker (`null` until the first frame).
     * @param {number} now_ms
     * @returns {string}
     */
    audio_tick(now_ms) {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.company_audio_tick(this.__wbg_ptr, now_ms);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Build ONE synthesis piece; returns pieces REMAINING (0 = ready to upload
     * buffers + tick). The host pumps one piece per task, so no frame waits on
     * the whole synthesis.
     * @returns {number}
     */
    audio_warmup_step() {
        const ret = wasm.company_audio_warmup_step(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Drop the hero's scripted cast, so only the issues this office is given show.
     */
    clear_script() {
        wasm.company_clear_script(this.__wbg_ptr);
    }
    /**
     * Whether the last written frame repainted everywhere; when false, only the rects at `dirty_ptr` changed.
     * @returns {boolean}
     */
    dirty_full() {
        const ret = wasm.company_dirty_full(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * The number of `u32`s at `dirty_ptr`.
     * @returns {number}
     */
    dirty_len() {
        const ret = wasm.company_dirty_len(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Pointer to the changed rects of the last written frame: `dirty_len` `u32`s, four per rect (`x, y, w, h`).
     * @returns {number}
     */
    dirty_ptr() {
        const ret = wasm.company_dirty_ptr(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Byte length of the RGBA frame: `w`×`h` units × [`Company::scale`]² × 4.
     * @returns {number}
     */
    frame_len() {
        const ret = wasm.company_frame_len(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Pointer to the RGBA frame in wasm linear memory ([`Company::frame_len`]
     * bytes).
     *
     * CONTRACT: re-read this (and rebuild any `Uint8ClampedArray` view) after
     * EVERY `step` — a canvas resize reallocates the staging buffer, and any
     * wasm `memory.grow` invalidates existing JS views into linear memory even
     * when the pointer value is unchanged.
     * @returns {number}
     */
    frame_ptr() {
        const ret = wasm.company_frame_ptr(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Hire one more agent: a new coworker walks into the background office,
     * works a few spells, and heads out `script::HIRE_STAY_MS` later. Refused
     * (`false`) before the first `step` (no clock yet), while `MAX_LIVE` hires
     * are already alive, and when the canvas-sized office has no free desk.
     * Never throws.
     * @returns {boolean}
     */
    hire() {
        const ret = wasm.company_hire(this.__wbg_ptr);
        return ret !== 0;
    }
    /**
     * Whether the office's sky shows the SUN at hour-of-day `hour` (0..24) — the
     * site's sky-slider thumb reads this, so it delegates to the engine's ONE
     * day/night boundary rather than restating it.
     * @param {number} hour
     * @returns {boolean}
     */
    is_day(hour) {
        const ret = wasm.company_is_day(this.__wbg_ptr, hour);
        return ret !== 0;
    }
    /**
     * The worker for `key` leaves its desk.
     * @param {string} key
     */
    issue_end(key) {
        const ptr0 = passStringToWasm0(key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.company_issue_end(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * Whether a worker for `key` is still in the office: a worker that has
     * been ended stays until it has walked out of the door.
     * @param {string} key
     * @returns {boolean}
     */
    issue_present(key) {
        const ptr0 = passStringToWasm0(key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.company_issue_present(this.__wbg_ptr, ptr0, len0);
        return ret !== 0;
    }
    /**
     * Seat a worker for the issue `key` at a free desk of office room `room`
     * (0..[`OFFICE_ROOMS`], or [`BOSS_ROOM`] for the boss's desk). `seated` puts it at its desk from the first frame;
     * otherwise it walks in from the door. `false` before the first `step`,
     * when that room has no desk left, or when the office has none left.
     * @param {string} key
     * @param {number} room
     * @param {boolean} seated
     * @returns {boolean}
     */
    issue_start(key, room, seated) {
        const ptr0 = passStringToWasm0(key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        const ret = wasm.company_issue_start(this.__wbg_ptr, ptr0, len0, room, seated);
        return ret !== 0;
    }
    /**
     * The worker for `key` works (`true`) or sits idle (`false`).
     * @param {string} key
     * @param {boolean} working
     */
    issue_working(key, working) {
        const ptr0 = passStringToWasm0(key, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.company_issue_working(this.__wbg_ptr, ptr0, len0, working);
    }
    /**
     * Build an office seeded with `seed` (drives the layout variant).
     *
     * # Errors
     *
     * If the bundled sprite pack fails to parse.
     * @param {number} seed
     */
    constructor(seed) {
        const ret = wasm.company_new(seed);
        if (ret[2]) {
            throw takeFromExternrefTable0(ret[1]);
        }
        this.__wbg_ptr = ret[0];
        CompanyFinalization.register(this, this.__wbg_ptr, this);
        return this;
    }
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
     * @returns {string}
     */
    overlay_json() {
        let deferred1_0;
        let deferred1_1;
        try {
            const ret = wasm.company_overlay_json(this.__wbg_ptr);
            deferred1_0 = ret[0];
            deferred1_1 = ret[1];
            return getStringFromWasm0(ret[0], ret[1]);
        } finally {
            wasm.__wbindgen_free(deferred1_0, deferred1_1, 1);
        }
    }
    /**
     * Frame pixels per layout unit `step` takes.
     * @returns {number}
     */
    scale() {
        const ret = wasm.company_scale(this.__wbg_ptr);
        return ret >>> 0;
    }
    /**
     * Hold the weather `name` names at midday for good: no night, no weather
     * cycle, no lightning, and the outside stands still, so its views are
     * drawn once. An unrecognized name keeps the policy as it was.
     * @param {string} name
     */
    set_daylight(name) {
        const ptr0 = passStringToWasm0(name, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.company_set_daylight(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * Recolor the whole office to one of the [`ALL_THEMES`](pixtuoid_scene::theme::ALL_THEMES) by name. Unknown
     * name = no-op.
     * @param {string} name
     */
    set_theme(name) {
        const ptr0 = passStringToWasm0(name, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        const len0 = WASM_VECTOR_LEN;
        wasm.company_set_theme(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * Force the weather [`WeatherPolicy::from_name`] names, or `None` (or an
     * unrecognized name) to follow the clock-based cycle.
     * @param {string | null} [name]
     */
    set_weather(name) {
        var ptr0 = isLikeNone(name) ? 0 : passStringToWasm0(name, wasm.__wbindgen_malloc, wasm.__wbindgen_realloc);
        var len0 = WASM_VECTOR_LEN;
        wasm.company_set_weather(this.__wbg_ptr, ptr0, len0);
    }
    /**
     * Advance to `now_ms` and render an office `w`×`h` layout units big into
     * the RGBA staging buffer, [`Company::scale`] pixels per unit. While the
     * flash hold keeps this frame back, the buffer keeps the last one.
     *
     * CONTRACT: `now_ms` must be UNIX-epoch milliseconds — `Date.now()`, NOT
     * `performance.now()` and NOT a `requestAnimationFrame` timestamp: those are
     * ms-since-page-load, which pins the day/night cycle and wall clock at 1970.
     * @param {number} now_ms
     * @param {number} w
     * @param {number} h
     */
    step(now_ms, w, h) {
        wasm.company_step(this.__wbg_ptr, now_ms, w, h);
    }
}
if (Symbol.dispose) Company.prototype[Symbol.dispose] = Company.prototype.free;
function __wbg_get_imports() {
    const import0 = {
        __proto__: null,
        __wbg_Error_30c8987f7c2ed4e2: function(arg0, arg1) {
            const ret = Error(getStringFromWasm0(arg0, arg1));
            return ret;
        },
        __wbg___wbindgen_throw_41e9ee4f547fc59a: function(arg0, arg1) {
            throw new Error(getStringFromWasm0(arg0, arg1));
        },
        __wbg_getTimezoneOffset_2e851fdaecc2dc92: function(arg0) {
            const ret = arg0.getTimezoneOffset();
            return ret;
        },
        __wbg_new_34e56228080aa9f8: function(arg0) {
            const ret = new Date(arg0);
            return ret;
        },
        __wbg_now_c734f200a0cc0a29: function() {
            const ret = performance.now();
            return ret;
        },
        __wbindgen_generic_0000000000000001: function(arg0) {
            // Cast intrinsic for `F64 -> Externref`.
            const ret = arg0;
            return ret;
        },
        __wbindgen_init_externref_table: function() {
            const table = wasm.__wbindgen_externrefs;
            const offset = table.grow(4);
            table.set(0, undefined);
            table.set(offset + 0, undefined);
            table.set(offset + 1, null);
            table.set(offset + 2, true);
            table.set(offset + 3, false);
        },
    };
    return {
        __proto__: null,
        "./pixtuoid_web_bg.js": import0,
    };
}

const CompanyFinalization = (typeof FinalizationRegistry === 'undefined')
    ? { register: () => {}, unregister: () => {} }
    : new FinalizationRegistry(ptr => wasm.__wbg_company_free(ptr, 1));

function getArrayU8FromWasm0(ptr, len) {
    ptr = ptr >>> 0;
    return getUint8ArrayMemory0().subarray(ptr / 1, ptr / 1 + len);
}

function getStringFromWasm0(ptr, len) {
    return decodeText(ptr >>> 0, len);
}

let cachedUint8ArrayMemory0 = null;
function getUint8ArrayMemory0() {
    if (cachedUint8ArrayMemory0 === null || cachedUint8ArrayMemory0.byteLength === 0) {
        cachedUint8ArrayMemory0 = new Uint8Array(wasm.memory.buffer);
    }
    return cachedUint8ArrayMemory0;
}

function isLikeNone(x) {
    return x === undefined || x === null;
}

function passStringToWasm0(arg, malloc, realloc) {
    if (realloc === undefined) {
        const buf = cachedTextEncoder.encode(arg);
        const ptr = malloc(buf.length, 1) >>> 0;
        getUint8ArrayMemory0().subarray(ptr, ptr + buf.length).set(buf);
        WASM_VECTOR_LEN = buf.length;
        return ptr;
    }

    let len = arg.length;
    let ptr = malloc(len, 1) >>> 0;

    const mem = getUint8ArrayMemory0();

    let offset = 0;

    for (; offset < len; offset++) {
        const code = arg.charCodeAt(offset);
        if (code > 0x7F) break;
        mem[ptr + offset] = code;
    }
    if (offset !== len) {
        if (offset !== 0) {
            arg = arg.slice(offset);
        }
        ptr = realloc(ptr, len, len = offset + arg.length * 3, 1) >>> 0;
        const view = getUint8ArrayMemory0().subarray(ptr + offset, ptr + len);
        const ret = cachedTextEncoder.encodeInto(arg, view);

        offset += ret.written;
        ptr = realloc(ptr, len, offset, 1) >>> 0;
    }

    WASM_VECTOR_LEN = offset;
    return ptr;
}

function takeFromExternrefTable0(idx) {
    const value = wasm.__wbindgen_externrefs.get(idx);
    wasm.__externref_table_dealloc(idx);
    return value;
}

let cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
cachedTextDecoder.decode();
const MAX_SAFARI_DECODE_BYTES = 2146435072;
let numBytesDecoded = 0;
function decodeText(ptr, len) {
    numBytesDecoded += len;
    if (numBytesDecoded >= MAX_SAFARI_DECODE_BYTES) {
        cachedTextDecoder = new TextDecoder('utf-8', { ignoreBOM: true, fatal: true });
        cachedTextDecoder.decode();
        numBytesDecoded = len;
    }
    return cachedTextDecoder.decode(getUint8ArrayMemory0().subarray(ptr, ptr + len));
}

const cachedTextEncoder = new TextEncoder();

if (!('encodeInto' in cachedTextEncoder)) {
    cachedTextEncoder.encodeInto = function (arg, view) {
        const buf = cachedTextEncoder.encode(arg);
        view.set(buf);
        return {
            read: arg.length,
            written: buf.length
        };
    };
}

let WASM_VECTOR_LEN = 0;

let wasmModule, wasmInstance, wasm;
function __wbg_finalize_init(instance, module) {
    wasmInstance = instance;
    wasm = instance.exports;
    wasmModule = module;
    cachedUint8ArrayMemory0 = null;
    wasm.__wbindgen_start();
    return wasm;
}

async function __wbg_load(module, imports) {
    if (typeof Response === 'function' && module instanceof Response) {
        if (!module.ok) {
            throw new Error(`failed to fetch Wasm: ${module.status} ${module.statusText} fetching '${module.url}'`);
        }

        if (typeof WebAssembly.instantiateStreaming === 'function') {
            try {
                return await WebAssembly.instantiateStreaming(module, imports);
            } catch (e) {
                const validResponse = expectedResponseType(module.type);

                if (validResponse && module.headers.get('Content-Type') !== 'application/wasm') {
                    console.warn("`WebAssembly.instantiateStreaming` failed because your server does not serve Wasm with `application/wasm` MIME type. Falling back to `WebAssembly.instantiate` which is slower. Original error:\n", e);

                } else { throw e; }
            }
        }

        const bytes = await module.arrayBuffer();
        return await WebAssembly.instantiate(bytes, imports);
    } else {
        const instance = await WebAssembly.instantiate(module, imports);

        if (instance instanceof WebAssembly.Instance) {
            return { instance, module };
        } else {
            return instance;
        }
    }

    function expectedResponseType(type) {
        switch (type) {
            case 'basic': case 'cors': case 'default': return true;
        }
        return false;
    }
}

function initSync(module) {
    if (wasm !== undefined) return wasm;


    if (module !== undefined) {
        if (Object.getPrototypeOf(module) === Object.prototype) {
            ({module} = module)
        } else {
            console.warn('using deprecated parameters for `initSync()`; pass a single object instead')
        }
    }

    const imports = __wbg_get_imports();
    if (!(module instanceof WebAssembly.Module)) {
        module = new WebAssembly.Module(module);
    }
    const instance = new WebAssembly.Instance(module, imports);
    return __wbg_finalize_init(instance, module);
}

async function __wbg_init(module_or_path) {
    if (wasm !== undefined) return wasm;


    if (module_or_path !== undefined) {
        if (Object.getPrototypeOf(module_or_path) === Object.prototype) {
            ({module_or_path} = module_or_path)
        } else {
            console.warn('using deprecated parameters for the initialization function; pass a single object instead')
        }
    }

    if (module_or_path === undefined) {
        module_or_path = new URL('pixtuoid_web_bg.wasm', import.meta.url);
    }
    const imports = __wbg_get_imports();

    if (typeof module_or_path === 'string' || (typeof Request === 'function' && module_or_path instanceof Request) || (typeof URL === 'function' && module_or_path instanceof URL)) {
        module_or_path = fetch(module_or_path);
    }

    const { instance, module } = await __wbg_load(await module_or_path, imports);

    return __wbg_finalize_init(instance, module);
}

export { initSync, __wbg_init as default };
