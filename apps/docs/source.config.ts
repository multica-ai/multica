import { defineDocs, defineConfig } from "fumadocs-mdx/config";
import { MARKDOWN_PLACEHOLDERS, markdownElementVisibility } from "./lib/markdown-export";
export const docs = defineDocs({
  dir: "content/docs",
  docs: {
    postprocess: {
      // Each page's Markdown, served by the `llms.txt` and `.md` routes.
      includeProcessedMarkdown: {
        headingIds: false,
        mdxAsPlaceholder: [...MARKDOWN_PLACEHOLDERS],
        filterElement: markdownElementVisibility,
      },
    },
  },
});

export default defineConfig({
  mdxOptions: {},
});
