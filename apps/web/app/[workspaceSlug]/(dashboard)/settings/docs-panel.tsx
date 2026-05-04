import { source } from "@/lib/source";
import { DocsPanelClient, type DocPage } from "./docs-panel-client";

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
      content: <Body />,
    };
  });
  return <DocsPanelClient pages={pages} />;
}
