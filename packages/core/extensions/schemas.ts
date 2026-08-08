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
});

export const PlatformExtensionAgentMappingSchema = z.object({
  source_key: z.string().min(1),
  id: UUIDSchema,
  name: z.string().min(1),
  leader: z.boolean(),
});

export const PlatformExtensionSkillMappingSchema = z.object({
  source_key: z.string().min(1),
  id: UUIDSchema,
  name: z.string().min(1),
});

export const PlatformExtensionMappingSchema = z.object({
  release: PlatformExtensionReleaseSchema,
  runtime: PlatformExtensionRuntimeSchema,
  squad: PlatformExtensionSquadSchema,
  agents: z.array(PlatformExtensionAgentMappingSchema),
  skills: z.array(PlatformExtensionSkillMappingSchema),
});

export const PlatformExtensionImportResultSchema =
  PlatformExtensionMappingSchema.extend({ idempotent: z.boolean() });

export const PlatformExtensionDetailSchema = PlatformExtensionMappingSchema.extend({
  manifest: z.record(z.string(), JSONValueSchema),
});

export const PlatformExtensionListSchema = z.array(PlatformExtensionMappingSchema);
