import contract from "@multica/computer-use/contract/computer.json";

/** Computer capability this Desktop implements; see the contract lock test. */
export const COMPUTER_CAPABILITY_VERSION: string = contract.version;
/** JSON-RPC channel version spoken with the daemon. */
export const COMPUTER_HOST_PROTOCOL_VERSION: string = contract.protocolVersion;

/** Semver with equal major and minor; a patch difference is compatible. */
export function compatibleVersion(a: unknown, b: unknown): boolean {
  const majorMinor = (value: unknown) => typeof value === "string" && /^\d+\.\d+\.\d+$/.test(value) ? value.split(".").slice(0, 2).join(".") : null;
  const x = majorMinor(a);
  return x !== null && x === majorMinor(b);
}
