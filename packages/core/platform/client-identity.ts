import type { ClientIdentity } from "./types";

/**
 * Module-level record of the identity the host app booted with (web/desktop +
 * version + os). CoreProvider stamps it once during init; headless consumers
 * (e.g. the server-version-mismatch hook) read it without threading props
 * through every layer.
 */
let currentIdentity: ClientIdentity | undefined;

export function setClientIdentity(identity: ClientIdentity | undefined): void {
  currentIdentity = identity;
}

export function getClientIdentity(): ClientIdentity | undefined {
  return currentIdentity;
}
