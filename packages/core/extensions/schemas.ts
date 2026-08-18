import { z } from "zod";

type JSONValue = null | boolean | number | string | JSONValue[] | { [key: string]: JSONValue };

const JSONValueSchema: z.ZodType<JSONValue> = z.lazy(() =>
  z.union([
    z.null(),
    z.boolean(),
    z.number(),
    z.string(),
    z.array(JSONValueSchema),
    z.record(z.string(), JSONValueSchema),
  ]),
);

const UUIDSchema = z.string().uuid();
const DigestSchema = z.string().regex(/^sha256:[0-9a-f]{64}$/);

export const PlatformExtensionReleaseSchema = z.object({
  id: UUIDSchema,
  extension_key: z.string().min(1),
  version: z.string().min(1),
  digest: DigestSchema,
});

export const PlatformExtensionRuntimeSchema = z.object({
  id: UUIDSchema,
  provider: z.literal("platform-agent-cli"),
  name: z.string().min(1),
});

export const PlatformExtensionSquadSchema = z.object({
  id: UUIDSchema,
  name: z.string().min(1),
  archived: z.boolean().default(false),
});

export const PlatformExtensionAgentMappingSchema = z.object({
  source_key: z.string().min(1),
  id: UUIDSchema,
  name: z.string().min(1),
  leader: z.boolean(),
  runtime: PlatformExtensionRuntimeSchema.nullable(),
});

export const PlatformExtensionSkillMappingSchema = z.object({
  source_key: z.string().min(1),
  id: UUIDSchema,
  name: z.string().min(1),
});

export const PlatformExtensionMappingSchema = z.object({
  release: PlatformExtensionReleaseSchema,
  runtime: PlatformExtensionRuntimeSchema.nullable(),
  squad: PlatformExtensionSquadSchema,
  agents: z.array(PlatformExtensionAgentMappingSchema),
  skills: z.array(PlatformExtensionSkillMappingSchema),
});

export const PlatformExtensionImportResultSchema =
  PlatformExtensionMappingSchema.extend({ idempotent: z.boolean() });

const PlatformExtensionManifestCommandSchema = z.object({
  name: z.string().min(1),
  description: z.string().optional(),
  content: z.string().optional(),
  metadata: z.record(z.string(), JSONValueSchema).optional(),
}).loose();

const PlatformExtensionManifestAgentSchema = z.object({
  key: z.string().optional(),
  name: z.string().min(1),
  description: z.string().optional(),
  prompt: z.string().optional(),
}).loose();

const PlatformExtensionManifestSkillFileSchema = z.object({
  path: z.string().min(1),
  content: z.string().optional(),
  encoding: z.literal("base64").optional(),
}).loose();

const PlatformExtensionManifestSkillSchema = z.object({
  key: z.string().optional(),
  name: z.string().min(1),
  description: z.string().optional(),
  files: z.array(PlatformExtensionManifestSkillFileSchema).optional(),
}).loose();

export const PlatformExtensionManifestSchema = z.object({
  extension: z.object({
    key: z.string().optional(),
    name: z.string().optional(),
    version: z.string().optional(),
    description: z.string().optional(),
  }).loose().optional(),
  leader: z.string().optional(),
  agents: z.array(PlatformExtensionManifestAgentSchema).optional(),
  skills: z.array(PlatformExtensionManifestSkillSchema).optional(),
  flow_commands: z.array(PlatformExtensionManifestCommandSchema).optional(),
  runtime_commands: z.array(PlatformExtensionManifestCommandSchema).optional(),
}).loose();

export const PlatformExtensionPreviewSchema = z.object({
  release: PlatformExtensionReleaseSchema.omit({ id: true }),
  squad_base_name: z.string().min(1),
  agents: z.array(z.object({
    source_key: z.string().min(1),
    name: z.string().min(1),
    leader: z.boolean(),
    runtime_id: z.union([UUIDSchema, z.literal("")]),
  })),
  runtimes: z.array(PlatformExtensionRuntimeSchema),
  manifest: PlatformExtensionManifestSchema,
});

export const PlatformExtensionDetailSchema = PlatformExtensionMappingSchema.extend({
  manifest: PlatformExtensionManifestSchema,
  available_runtimes: z.array(PlatformExtensionRuntimeSchema).default([]),
});

export const PlatformExtensionListSchema = z.array(PlatformExtensionMappingSchema);
