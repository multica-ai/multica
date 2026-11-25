import { asLang, i18n } from "@/lib/i18n";
import { docsIndexMarkdown, markdownResponse } from "@/lib/llms";

export const revalidate = false;

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export async function GET(_request: Request, { params }: { params: Promise<{ lang: string }> }) {
  const { lang } = await params;
  return markdownResponse(await docsIndexMarkdown(asLang(lang)));
}
