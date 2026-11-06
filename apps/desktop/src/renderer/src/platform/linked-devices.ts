import { z } from "zod";
import { useQuery } from "@tanstack/react-query";
import { parseWithFallback } from "@multica/core/api/schema";
import { localV2Transport } from "./local-v2-transport";

const linkedDevicesSchema = z.object({
  devices: z.array(z.object({ peer_id: z.string().min(1), device_name: z.string().default(""), connection: z.string().default("") })).default([]),
});
export type LinkedDevice = z.infer<typeof linkedDevicesSchema>["devices"][number];

const NO_DEVICES: LinkedDevice[] = [];

/** How often the sidebar learns which devices share their conversations. */
const POLL_MS = 5_000;

/**
 * The account's other devices that are connected and share their
 * conversations over Device link, as this device's daemon sees them.
 */
export function useLinkedDevicesQuery(enabled = true) {
  return useQuery({
    queryKey: ["device-link", "devices"],
    queryFn: async () => parseWithFallback<z.infer<typeof linkedDevicesSchema>>(await localV2Transport().devices(), linkedDevicesSchema, { devices: [] }, { endpoint: "local.devices" }).devices,
    enabled,
    refetchInterval: POLL_MS,
    retry: false,
  });
}

export function useLinkedDevices(enabled = true): LinkedDevice[] {
  return useLinkedDevicesQuery(enabled).data ?? NO_DEVICES;
}

/** A device's display name, falling back to a short form of its ID. */
export function deviceName(devices: LinkedDevice[], peerId: string): string {
  return devices.find((device) => device.peer_id === peerId)?.device_name || peerId.slice(-6);
}
