import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { shouldDropException } from "./exception-dedupe";

const STORAGE_KEY = "mc_exc_fp";

// In-memory sessionStorage stand-in. Optional flags let a test force getItem /
// setItem to throw (quota, disabled storage) so we can assert the fail-open
// direction.
function makeStorage(opts: { throwOnGet?: boolean; throwOnSet?: boolean } = {}) {
  const data = new Map<string, string>();
  return {
    data,
    getItem(k: string): string | null {
      if (opts.throwOnGet) throw new Error("getItem blocked");
      return data.has(k) ? data.get(k)! : null;
    },
    setItem(k: string, v: string): void {
      if (opts.throwOnSet) throw new Error("quota exceeded");
      data.set(k, v);
    },
    removeItem(k: string): void {
      data.delete(k);
    },
    clear(): void {
      data.clear();
    },
    key(i: number): string | null {
      return Array.from(data.keys())[i] ?? null;
    },
    get length(): number {
      return data.size;
    },
  };
}

// Build a redacted-shape `$exception` properties object. By the time dedupe
// runs, redactExceptionProperties has already scrubbed value/message.
function exc(o: {
  type?: string;
  value?: string;
  frames?: Array<Record<string, unknown>> | null;
} = {}): Record<string, unknown> {
  const entry: Record<string, unknown> = {
    type: o.type ?? "TypeError",
    value: o.value ?? "boom",
  };
  if (o.frames !== null) {
    entry.stacktrace = {
      type: "raw",
      frames: o.frames ?? [
        { filename: "app.tsx", function: "render", lineno: 10, colno: 5 },
      ],
    };
  }
  return { $exception_list: [entry] };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("shouldDropException — per-fingerprint limit", () => {
  beforeEach(() => {
    vi.stubGlobal("sessionStorage", makeStorage());
  });

  it("keeps the first 3 of a fingerprint and drops from the 4th", () => {
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(true);
    expect(shouldDropException(exc())).toBe(true);
  });

  it("treats different fingerprints independently — one does not drop the other", () => {
    // Exhaust fingerprint A.
    const a = () => exc({ type: "TypeError", value: "a" });
    const b = () => exc({ type: "RangeError", value: "b" });
    shouldDropException(a());
    shouldDropException(a());
    shouldDropException(a());
    expect(shouldDropException(a())).toBe(true); // A fused
    // B is untouched.
    expect(shouldDropException(b())).toBe(false);
    expect(shouldDropException(b())).toBe(false);
    expect(shouldDropException(b())).toBe(false);
    expect(shouldDropException(b())).toBe(true);
  });

  it("discriminates on colno (minified bundles collapse statements onto one line)", () => {
    const at = (colno: number) =>
      exc({ frames: [{ filename: "b.js", function: "x", lineno: 1, colno }] });
    // Same file/line/function, different column → distinct fingerprints, so
    // each keeps its own first-3 budget.
    shouldDropException(at(10));
    shouldDropException(at(10));
    shouldDropException(at(10));
    expect(shouldDropException(at(10))).toBe(true);
    expect(shouldDropException(at(20))).toBe(false);
  });

  it("stores only a hash + counter — no raw value reaches storage", () => {
    const storage = makeStorage();
    vi.stubGlobal("sessionStorage", storage);
    shouldDropException(exc({ value: "secret-marker-12345" }));
    const blob = storage.data.get(STORAGE_KEY) ?? "";
    expect(blob).not.toContain("secret-marker-12345");
    expect(blob).not.toContain("app.tsx");
  });
});

describe("shouldDropException — degraded frames", () => {
  beforeEach(() => {
    vi.stubGlobal("sessionStorage", makeStorage());
  });

  it("tolerates missing lineno/colno/function and still dedupes", () => {
    const partial = () => exc({ frames: [{ filename: "only-file.js" }] });
    expect(() => shouldDropException(partial())).not.toThrow();
    shouldDropException(partial());
    shouldDropException(partial());
    expect(shouldDropException(partial())).toBe(true);
  });

  it("tolerates no stacktrace at all (fingerprints on type + value)", () => {
    const noframes = () => exc({ frames: null });
    shouldDropException(noframes());
    shouldDropException(noframes());
    shouldDropException(noframes());
    expect(shouldDropException(noframes())).toBe(true);
  });

  it("keeps events with no usable signal (empty type/value/frames)", () => {
    const empty = { $exception_list: [{ type: "", value: "" }] };
    expect(shouldDropException(empty)).toBe(false);
    expect(shouldDropException(empty)).toBe(false);
    expect(shouldDropException(empty)).toBe(false);
    expect(shouldDropException(empty)).toBe(false); // never fused — no fingerprint
  });

  it("is safe on undefined / malformed properties", () => {
    expect(shouldDropException(undefined)).toBe(false);
    expect(
      shouldDropException({ $exception_list: "nope" as unknown as [] }),
    ).toBe(false);
  });
});

describe("shouldDropException — storage fail-open", () => {
  it("fails open when sessionStorage is undefined (SSR)", () => {
    vi.stubGlobal("sessionStorage", undefined);
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(false);
    expect(shouldDropException(exc())).toBe(false);
  });

  it("fails open when accessing sessionStorage throws (sandboxed iframe)", () => {
    Object.defineProperty(globalThis, "sessionStorage", {
      configurable: true,
      get() {
        throw new Error("blocked by sandbox");
      },
    });
    try {
      expect(() => shouldDropException(exc())).not.toThrow();
      expect(shouldDropException(exc())).toBe(false);
    } finally {
      // Remove the throwing getter so it doesn't leak into other tests.
      Object.defineProperty(globalThis, "sessionStorage", {
        configurable: true,
        value: undefined,
      });
    }
  });

  it("fails open when getItem throws", () => {
    vi.stubGlobal("sessionStorage", makeStorage({ throwOnGet: true }));
    expect(() => shouldDropException(exc())).not.toThrow();
    expect(shouldDropException(exc())).toBe(false);
  });

  it("fails open on a corrupted JSON blob and re-seeds clean state", () => {
    const storage = makeStorage();
    storage.data.set(STORAGE_KEY, "{not valid json");
    vi.stubGlobal("sessionStorage", storage);

    expect(shouldDropException(exc())).toBe(false);
    // Blob is now valid JSON again with this fingerprint counted once.
    const reseeded = JSON.parse(storage.data.get(STORAGE_KEY)!);
    expect(typeof reseeded).toBe("object");
    expect(Object.values(reseeded)).toEqual([1]);
  });

  it("setItem failure under-counts (fewer drops), never over-drops", () => {
    vi.stubGlobal("sessionStorage", makeStorage({ throwOnSet: true }));
    // Persisting the increment always fails, so the counter never advances and
    // no event is ever dropped — the required "less drop" direction.
    for (let i = 0; i < 5; i++) {
      expect(shouldDropException(exc())).toBe(false);
    }
  });
});

describe("shouldDropException — distinct-fingerprint cap", () => {
  it("keeps (does not track) a new fingerprint once the cap is reached", () => {
    const storage = makeStorage();
    // Seed 50 distinct fingerprints already at count 1.
    const seed: Record<string, number> = {};
    for (let i = 0; i < 50; i++) seed[`seed-${i}`] = 1;
    storage.data.set(STORAGE_KEY, JSON.stringify(seed));
    vi.stubGlobal("sessionStorage", storage);

    // The 51st, brand-new fingerprint is kept and NOT added to the blob.
    expect(shouldDropException(exc({ value: "fingerprint-51" }))).toBe(false);
    const after = JSON.parse(storage.data.get(STORAGE_KEY)!);
    expect(Object.keys(after)).toHaveLength(50);
  });

  it("still fuses a fingerprint that is already tracked at the cap", () => {
    const storage = makeStorage();
    const seed: Record<string, number> = {};
    for (let i = 0; i < 49; i++) seed[`seed-${i}`] = 1;
    vi.stubGlobal("sessionStorage", storage);

    // Track a real one to reach 50 distinct, exhausting its budget.
    const target = () => exc({ value: "tracked-at-cap" });
    storage.data.set(STORAGE_KEY, JSON.stringify(seed));
    shouldDropException(target()); // 50th distinct, count 1
    shouldDropException(target()); // 2
    shouldDropException(target()); // 3
    expect(shouldDropException(target())).toBe(true); // fused despite cap
  });
});
