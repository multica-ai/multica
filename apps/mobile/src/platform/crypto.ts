export interface CryptoSource {
  getRandomValues<T extends Uint8Array>(array: T): T;
  randomUUID(): string;
}

type CryptoHost = { crypto?: Partial<Pick<Crypto, "getRandomValues" | "randomUUID">> };

/** Fills in whichever of the two Web Crypto functions the runtime lacks; a native implementation is kept. */
export function installCrypto(host: CryptoHost, source: CryptoSource): void {
  const target = host.crypto ?? {};
  if (typeof target.getRandomValues !== "function") {
    target.getRandomValues = ((array: Uint8Array) => source.getRandomValues(array)) as Crypto["getRandomValues"];
  }
  if (typeof target.randomUUID !== "function") {
    target.randomUUID = (() => source.randomUUID()) as Crypto["randomUUID"];
  }
  if (!host.crypto) Object.defineProperty(host, "crypto", { value: target, configurable: true, writable: true });
}
