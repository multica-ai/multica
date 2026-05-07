import type { ReactNode } from "react";
import { source } from "@/lib/source";
import { DocsPanelClient, type DocPage } from "./docs-panel-client";
// CEREBRO-PATCH(docs-panel-mdx-shims): see ./cerebro-mdx-shims.tsx for rationale.
import { cerebroMdxComponents } from "./cerebro-mdx-shims";

/**
 * Multica-styled Callout — used by some MDX content (e.g. self-hosting docs)
 * that was originally written for fumadocs-ui. Shimming it here lets us drop
 * fumadocs-ui from the embedded view while still supporting the existing MDX.
 */
function Callout({ children }: { children: ReactNode }) {
  return (
    <div className="my-4 rounded-md border border-l-4 border-l-primary bg-muted/40 px-4 py-3 text-sm">
      {children}
    </div>
  );
}

const mdxComponents = { Callout, ...cerebroMdxComponents };

export function DocsPanel() {
  // source.getPages() returns all docs collected by fumadocs-mdx. We
  // pre-render each MDX body server-side and pass the resulting React
  // elements to the client component, which handles nav/active state.
  const pages: DocPage[] = source.getPages().map((page) => {
    const Body = page.data.body;
    const slugs = page.slugs;
    return {
      slug: slugs.length > 0 ? slugs.join("/") : "/",
      title: page.data.title,
      description: page.data.description,
      group: slugs.length > 1 ? slugs[0] : undefined,
      content: <Body components={mdxComponents} />,
    };
  });
  return <DocsPanelClient pages={pages} />;
}
