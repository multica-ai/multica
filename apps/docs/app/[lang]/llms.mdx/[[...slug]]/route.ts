import { asLang } from "@/lib/i18n";
import { source } from "@/lib/source";
import { absoluteDocsUrl } from "@/lib/site";
import { markdownResponse, pageMarkdown } from "@/lib/llms";

// Target of the docs proxy's `.md` and `Accept: text/markdown` rewrites.
export const revalidate = false;

export function generateStaticParams() {
  return source.generateParams();
}

export async function GET(
  _request: Request,
  { params }: { params: Promise<{ lang: string; slug?: string[] }> },
) {
  const { lang: rawLang, slug = [] } = await params;
  const lang = asLang(rawLang);
  const page = source.getPage(slug, lang);
  if (!page) return markdownResponse("# Not found\n", { status: 404 });
  return markdownResponse(await pageMarkdown(page, lang), {
    canonical: absoluteDocsUrl(page.url),
  });
}
