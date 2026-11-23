// @vitest-environment node
import { remarkLLMs } from "fumadocs-core/mdx-plugins/remark-llms";
import remarkGfm from "remark-gfm";
import remarkMdx from "remark-mdx";
import remarkParse from "remark-parse";
import { unified } from "unified";
import { describe, expect, it } from "vitest";
import {
  MARKDOWN_PLACEHOLDERS,
  absolutizeMarkdownLinks,
  markdownElementVisibility,
  renderPageMarkdown,
} from "./markdown-export";

const DOCS_URL = "https://www.multica.ai/docs";

/** The page's Markdown as `includeProcessedMarkdown` exports it with the options in `source.config.ts`. */
function processedMarkdown(source: string): string {
  const tree = unified().use(remarkParse).use(remarkMdx).use(remarkGfm).parse(source);
  const file = { data: {} as { markdown?: string }, toString: () => source };
  remarkLLMs({
    headingIds: false,
    mdxAsPlaceholder: [...MARKDOWN_PLACEHOLDERS],
    filterElement: markdownElementVisibility,
    _data: true,
  })(tree, file as never, () => undefined);
  return file.data.markdown ?? "";
}

const exportPage = (source: string, lang = "en") =>
  renderPageMarkdown(processedMarkdown(source), lang, DOCS_URL);

describe("page Markdown export", () => {
  it("turns docs components into plain Markdown and drops interactive demos", async () => {
    const markdown = await exportPage(`import { Callout } from "fumadocs-ui/components/callout";
import { AssignDemo } from "@/components/demos/assign-demo";

Intro with a [link](/providers).

<AssignDemo />

<Callout type="warning">
Back up first.
</Callout>

<Steps>
<Step>

## Install [#install]

Run it.

</Step>
<Step>

## Verify

Check it.

</Step>
</Steps>

<Tabs items={["macOS", "Windows"]}>
<Tab value="macOS">

\`\`\`bash
brew install multica
\`\`\`

</Tab>
</Tabs>

<Accordions>
<Accordion title={<><code>multica login</code> fails</>} id="login">

Retry.

</Accordion>
</Accordions>
`);

    expect(markdown).toBe(`Intro with a [link](https://www.multica.ai/docs/providers).

> **Warning:** Back up first.

## Install

Run it.

## Verify

Check it.

**macOS**

\`\`\`bash
brew install multica
\`\`\`

### \`multica login\` fails

Retry.`);
  });

  it("renders video embeds and community links as links", async () => {
    const markdown = await exportPage(`<VideoEmbed provider="bilibili" id="BV1cv7Y6gEg7" title="Intro" />

<CommunityLinks discordDescription="Chat." githubDescription="Code." xDescription="News." />
`);
    expect(markdown).toBe(`[Video: Intro](https://www.bilibili.com/video/BV1cv7Y6gEg7/)

- [Discord](https://discord.gg/W8gYBn226t): Chat.
- [GitHub](https://github.com/multica-ai/multica): Code.
- [X](https://x.com/MulticaAI): News.`);
  });

  it("keeps the text of unknown components and drops expressions", async () => {
    expect(await exportPage(`<Feature>Kept text</Feature>\n\n{/* note */}\n\nAfter.\n`)).toBe("Kept text\n\nAfter.");
  });
});

describe("absolutizeMarkdownLinks", () => {
  it("keeps page links in the page's language and images unprefixed", () => {
    expect(
      absolutizeMarkdownLinks("[A](/agents#setup) [B](/zh/issues) ![C](/images/docs/a.webp) [D](https://x.com)", "zh", DOCS_URL),
    ).toBe(
      "[A](https://www.multica.ai/docs/zh/agents#setup) [B](https://www.multica.ai/docs/zh/issues) ![C](https://www.multica.ai/docs/images/docs/a.webp) [D](https://x.com)",
    );
    expect(absolutizeMarkdownLinks("[Home](/) [A](/agents)", "en", DOCS_URL)).toBe(
      "[Home](https://www.multica.ai/docs) [A](https://www.multica.ai/docs/agents)",
    );
  });

  it("leaves code blocks untouched and collapses blank lines outside them", () => {
    const code = "```md\n[A](/agents)\n\n\n\nend\n```";
    expect(absolutizeMarkdownLinks(`a\n\n\n\nb\n\n${code}`, "en", DOCS_URL)).toBe(`a\n\nb\n\n${code}`);
  });
});
