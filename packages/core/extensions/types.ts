export const PLATFORM_EXTENSION_MAX_IMPORT_BYTES = 5 * 1024 * 1024;

export class PlatformExtensionDocumentTooLargeError extends Error {
  readonly maxBytes = PLATFORM_EXTENSION_MAX_IMPORT_BYTES;

  constructor() {
    super("Extension document exceeds the 5 MiB limit");
    this.name = "PlatformExtensionDocumentTooLargeError";
  }
}

export class PlatformExtensionDocumentEncodingError extends Error {
  constructor() {
    super("Extension document must be valid UTF-8");
    this.name = "PlatformExtensionDocumentEncodingError";
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
}

export interface PlatformExtensionAgentMapping {
  source_key: string;
  id: string;
  name: string;
  leader: boolean;
}

export interface PlatformExtensionSkillMapping {
  source_key: string;
  id: string;
  name: string;
}

export interface PlatformExtensionMapping {
  release: PlatformExtensionRelease;
  runtime: PlatformExtensionRuntime;
  squad: PlatformExtensionSquad;
  agents: PlatformExtensionAgentMapping[];
  skills: PlatformExtensionSkillMapping[];
}

export interface PlatformExtensionImportResult extends PlatformExtensionMapping {
  idempotent: boolean;
}

export interface PlatformExtensionDetail extends PlatformExtensionMapping {
  manifest: Record<string, unknown>;
}
