import { z } from "zod";
import type { AgentRuntime } from "../types";

export const AgentRuntimeSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  daemon_id: z.string().nullable().catch(null),
  name: z.string().catch(""),
  custom_name: z.string().nullable().catch(null),
  runtime_mode: z.enum(["local", "cloud"]).catch("local"),
  provider: z.string().catch(""),
  launch_header: z.string().catch(""),
  status: z.enum(["online", "offline"]).catch("offline"),
  device_info: z.string().catch(""),
  metadata: z.record(z.string(), z.unknown()).catch({}),
  owner_id: z.string().nullable().catch(null),
  visibility: z.enum(["private", "public"]).catch("private"),
  shared_with: z.array(z.string().uuid()).catch([]),
  profile_id: z.string().nullable().catch(null),
  last_seen_at: z.string().nullable().catch(null),
  created_at: z.string().catch(""),
  updated_at: z.string().catch(""),
}).loose();

export const EMPTY_RUNTIME: AgentRuntime = {
  id: "",
  workspace_id: "",
  daemon_id: null,
  name: "",
  runtime_mode: "local",
  provider: "",
  launch_header: "",
  status: "offline",
  device_info: "",
  metadata: {},
  owner_id: null,
  visibility: "private",
  shared_with: [],
  last_seen_at: null,
  created_at: "",
  updated_at: "",
};
