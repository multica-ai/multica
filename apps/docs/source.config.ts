import { defineDocs, defineConfig } from "fumadocs-mdx/config";
import { MARKDOWN_PLACEHOLDERS, markdownElementVisibility } from "./lib/markdown-export";
import { remarkDropIncludedTitle } from "./lib/remark-included-title";
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
  mdxOptions: {
    // Before the preset's heading plugins, so the dropped title stays out of the TOC.
    remarkPlugins: (plugins) => [remarkDropIncludedTitle, ...plugins],
  },
});
