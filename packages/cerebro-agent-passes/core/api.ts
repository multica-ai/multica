import { api } from "@multica/core/api";
import type {
  AgentPass,
  AgentPassListResponse,
  CreateAgentPassRequest,
  RevokeAgentPassRequest,
} from "./types";

export async function fetchAgentPasses(): Promise<AgentPassListResponse> {
  const raw = await api.listCerebroAgentPasses<AgentPassListResponse>();
  // Server always returns { passes: [] } — fall back to an empty list
  // on shape drift instead of throwing into the UI (per the conventions
  // in apps/docs/content/docs/developers/conventions.mdx).
  if (!raw || !Array.isArray((raw as AgentPassListResponse).passes)) {
    return { passes: [] };
  }
  return raw;
}

export async function createAgentPass(body: CreateAgentPassRequest): Promise<AgentPass> {
  return api.createCerebroAgentPass<AgentPass>(body);
}

export async function revokeAgentPass(
  passId: string,
  body?: RevokeAgentPassRequest,
): Promise<AgentPass> {
  return api.revokeCerebroAgentPass<AgentPass>(passId, body);
}
