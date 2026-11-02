import { readFile } from "fs/promises";
import { healthPortForProfile, profilePortPath } from "./daemon-profile";

export interface DaemonPortRecord {
  port: number;
  pid: number;
}

export function parseDaemonPortRecord(raw: string): DaemonPortRecord | null {
  try {
    const value: unknown = JSON.parse(raw);
    if (!value || typeof value !== "object") return null;
    const { port, pid } = value as Record<string, unknown>;
    if (
      typeof port !== "number" ||
      !Number.isInteger(port) ||
      port < 1 ||
      port > 65535 ||
      typeof pid !== "number" ||
      !Number.isSafeInteger(pid) ||
      pid < 1
    ) return null;
    return { port, pid };
  } catch {
    return null;
  }
}

export async function readDaemonPortRecord(profile: string): Promise<DaemonPortRecord | null> {
  try {
    return parseDaemonPortRecord(await readFile(profilePortPath(profile), "utf-8"));
  } catch {
    return null;
  }
}

interface ProfileHealth {
  status?: string;
  profile?: string;
  pid?: number;
}

// The record can survive a crash or a supervisor handoff. Probe it on every
// call; only a live daemon with the expected identity is allowed to replace the
// profile's legacy port. This also lets Desktop follow a changed port without
// rebuilding its cached profile.
export async function probeProfileHealth<T extends ProfileHealth>(
  profile: string,
  probe: (port: number) => Promise<T | null>,
  readRecord: () => Promise<DaemonPortRecord | null> = () => readDaemonPortRecord(profile),
): Promise<T | null> {
  const record = await readRecord();
  if (record) {
    const health = await probe(record.port);
    if (
      (health?.status === "running" || health?.status === "starting") &&
      health.profile === profile &&
      health.pid === record.pid
    ) return health;
  }
  const health = await probe(healthPortForProfile(profile));
  if (health?.profile !== undefined && health.profile !== profile) return null;
  return health;
}
