export type LifeOSHostState =
  | "disabled"
  | "starting"
  | "ready"
  | "offline"
  | "error";

export interface LifeOSHostStatus {
  state: LifeOSHostState;
  checkedAt: number;
  message?: string;
}

export const LIFEOS_HOST_STATUS_CHANNEL = "lifeos-host:status";
export const LIFEOS_HOST_GET_STATUS_CHANNEL = "lifeos-host:get-status";
export const LIFEOS_HOST_ENSURE_CHANNEL = "lifeos-host:ensure";
export const LIFEOS_HOST_OPEN_LOGS_CHANNEL = "lifeos-host:open-logs";
