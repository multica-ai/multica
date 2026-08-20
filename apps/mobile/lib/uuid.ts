import { randomUUID as expoRandomUUID } from "expo-crypto";

/** UUID v4 for durable idempotency keys, backed by native secure randomness. */
export function randomUUID(): string {
  return expoRandomUUID();
}
