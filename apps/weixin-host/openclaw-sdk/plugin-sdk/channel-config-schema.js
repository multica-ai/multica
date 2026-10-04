// The host does not render plugin config UI, so the zod schema is kept only
// for reference.
export function buildChannelConfigSchema(zodSchema) {
  return { schema: { type: "object", additionalProperties: true }, zod: zodSchema };
}
