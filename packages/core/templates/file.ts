import { MarketplaceTemplateFileSchema } from "../api/schemas";
import type {
  MarketplaceTemplateFileV1,
  MarketplaceTemplateFileV2,
  NormalizedMarketplaceTemplateFile,
} from "../types";

export type MarketplaceTemplateFileParseResult =
  | { success: true; file: NormalizedMarketplaceTemplateFile }
  | { success: false };

function normalizeV2TemplateFile(file: MarketplaceTemplateFileV2): MarketplaceTemplateFileV1 {
  const squad = file.type === "squad" && file.spec
    ? {
        name: file.spec.name,
        description: file.spec.description,
        instructions: file.spec.instructions ?? "",
        leader_key: file.spec.leader_ref,
        members: file.spec.members.map((member) => ({
          agent_key: member.agent_ref,
          role: member.role,
        })),
      }
    : undefined;
  return {
    format: "multica-template",
    version: 1,
    exported_at: "",
    name: file.metadata.name,
    description: file.metadata.description,
    tags: file.metadata.tags,
    source_type: file.type,
    snapshot_version: 1,
    snapshot: {
      version: 1,
      source_type: file.type,
      agents: file.resources.agents.map((agent) => ({
        key: agent.key,
        name: agent.name,
        description: agent.description,
        instructions: agent.instructions,
        conversation_starters: agent.conversation_starters ?? [],
        max_concurrent_tasks: agent.max_concurrent_tasks,
        skill_keys: agent.skill_refs,
      })),
      skills: file.resources.skills.map((skill) => ({
        key: skill.key,
        name: skill.name,
        description: skill.description,
        content: skill.content,
        config: skill.config ?? {},
        files: skill.files,
      })),
      squad,
    },
  };
}

export function parseMarketplaceTemplateFile(input: unknown): MarketplaceTemplateFileParseResult {
  const parsed = MarketplaceTemplateFileSchema.safeParse(input);
  if (!parsed.success) return { success: false };
  if (parsed.data.format === "multica-template") {
    return { success: true, file: parsed.data as MarketplaceTemplateFileV1 };
  }
  return {
    success: true,
    file: normalizeV2TemplateFile(parsed.data as MarketplaceTemplateFileV2),
  };
}
