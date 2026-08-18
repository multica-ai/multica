export const PLATFORM_EXTENSION_MAX_IMPORT_BYTES = 16 * 1024 * 1024;
export const PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX = "-e2e";

export type PlatformExtensionCommandKind = "flow" | "skill";

export interface PlatformExtensionManifestCommand {
  name: string;
  description?: string;
  content?: string;
  metadata?: Record<string, unknown>;
}

export interface PlatformExtensionManifestAgent {
  key?: string;
  name: string;
  description?: string;
  prompt?: string;
}

export interface PlatformExtensionManifestSkillFile {
  path: string;
  content?: string;
  encoding?: "base64";
}

export interface PlatformExtensionManifestSkill {
  key?: string;
  name: string;
  description?: string;
  files?: PlatformExtensionManifestSkillFile[];
}

export interface PlatformExtensionManifest {
  extension?: { key?: string; name?: string; version?: string; description?: string };
  leader?: string;
  agents?: PlatformExtensionManifestAgent[];
  skills?: PlatformExtensionManifestSkill[];
  flow_commands?: PlatformExtensionManifestCommand[];
  runtime_commands?: PlatformExtensionManifestCommand[];
}

export function classifyPlatformExtensionCommand(
  commandName: string,
): PlatformExtensionCommandKind {
  return commandName.endsWith(PLATFORM_EXTENSION_FLOW_COMMAND_SUFFIX)
    ? "flow"
    : "skill";
}

export class PlatformExtensionDocumentTooLargeError extends Error {
  readonly maxBytes = PLATFORM_EXTENSION_MAX_IMPORT_BYTES;

  constructor() {
    super("Extension package exceeds the 16 MiB limit");
    this.name = "PlatformExtensionDocumentTooLargeError";
  }
}

export interface PlatformExtensionRelease {
  id: string;
  extension_key: string;
  version: string;
  digest: string;
}

export interface PlatformExtensionRuntime {
  id: string;
  provider: "platform-agent-cli";
  name: string;
}

export interface PlatformExtensionSquad {
  id: string;
  name: string;
  archived?: boolean;
}

export interface PlatformExtensionAgentMapping {
  source_key: string;
  id: string;
  name: string;
  leader: boolean;
  runtime: PlatformExtensionRuntime | null;
}

export interface PlatformExtensionSkillMapping {
  source_key: string;
  id: string;
  name: string;
}

export interface PlatformExtensionMapping {
  release: PlatformExtensionRelease;
  runtime: PlatformExtensionRuntime | null;
  squad: PlatformExtensionSquad;
  agents: PlatformExtensionAgentMapping[];
  skills: PlatformExtensionSkillMapping[];
}

export interface PlatformExtensionImportResult extends PlatformExtensionMapping {
  idempotent: boolean;
}

export interface PlatformExtensionImportConfiguration {
  squad_base_name: string;
  agent_runtime_ids: Record<string, string>;
}

export interface PlatformExtensionPreviewAgent {
  source_key: string;
  name: string;
  leader: boolean;
  runtime_id: string;
}

export interface PlatformExtensionPreview {
  release: Omit<PlatformExtensionRelease, "id">;
  squad_base_name: string;
  agents: PlatformExtensionPreviewAgent[];
  runtimes: PlatformExtensionRuntime[];
  manifest: PlatformExtensionManifest;
}

export interface PlatformExtensionDetail extends PlatformExtensionMapping {
  manifest: PlatformExtensionManifest;
  available_runtimes: PlatformExtensionRuntime[];
}
