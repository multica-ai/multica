import { defineDocs, defineConfig } from "fumadocs-mdx/config";
import { z } from "zod";

// The default fumadocs frontmatter schema is strict and strips any field
// outside `{ title, description, icon, full }`. Our use-case pages need
// `hero_image` and `updated_at` to render the index card and sort the
// list, plus an optional `category` label. They are declared here so they
// pass validation (and gain TypeScript types), while looseObject lets
// any ad-hoc field still pass through.
// YAML auto-parses bare ISO dates (`updated_at: 2026-05-19`) into Date
// objects, which then fail `z.string()`. Preprocess to normalize Date back
// to a `YYYY-MM-DD` string so authors can write either form.
const dateString = z.preprocess(
  (v) => (v instanceof Date ? v.toISOString().slice(0, 10) : v),
  z.string(),
);

const useCaseFrontmatterSchema = z.looseObject({
  title: z.string(),
  description: z.string().optional(),
  hero_image: z.string(),
  updated_at: dateString,
  category: z.string().optional(),
});

export const useCases = defineDocs({
  dir: "content/use-cases",
  docs: {
    schema: useCaseFrontmatterSchema,
  },
});

export default defineConfig({
  mdxOptions: {},
});
