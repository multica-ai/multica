export type RuntimePermissionRole = "owner" | "admin" | "operator" | "viewer" | "";

export interface RuntimeCapabilities {
  control: boolean;
  observe: boolean;
}

export interface MyRuntimePermissionResponse {
  role: RuntimePermissionRole;
  can_control: boolean;
  can_observe: boolean;
}

export interface RuntimePermission {
  id: string;
  runtime_id: string;
  user_id: string;
  role: RuntimePermissionRole;
  user_name?: string;
  user_email?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimePermissionListResponse {
  permissions: RuntimePermission[];
}

export interface CreateRuntimePermissionRequest {
  user_id: string;
  role: RuntimePermissionRole;
}

export interface UpdateRuntimePermissionRequest {
  role: RuntimePermissionRole;
}

export interface SessionPermissionResponse {
  workspace_id: string;
  node_run_id: string;
  device_id: string;
  session_id: string;
  role: RuntimePermissionRole;
  can_control: boolean;
  can_observe: boolean;
}
