import { describe, expect, it, vi } from "vitest";
import { installCrypto, type CryptoSource } from "./crypto";

const source: CryptoSource = {
  getRandomValues: vi.fn((array) => array.fill(7)),
  randomUUID: vi.fn(() => "00000000-0000-4000-8000-000000000000"),
};

describe("installCrypto", () => {
  it("provides both functions when the runtime has no crypto, as on Hermes", () => {
    const host: { crypto?: Crypto } = {};
    installCrypto(host, source);
    expect(host.crypto?.getRandomValues(new Uint8Array(2))).toEqual(new Uint8Array([7, 7]));
    expect(host.crypto?.randomUUID()).toBe("00000000-0000-4000-8000-000000000000");
  });

  it("keeps a native implementation and only fills the gap", () => {
    const native = vi.fn((array: Uint8Array) => array);
    const host = { crypto: { getRandomValues: native } as unknown as Crypto };
    installCrypto(host, source);
    expect(host.crypto.getRandomValues).toBe(native);
    expect(host.crypto.randomUUID()).toBe("00000000-0000-4000-8000-000000000000");
  });
});
