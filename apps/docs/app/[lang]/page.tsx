import { source } from "@/lib/source";
import { DocsPage, DocsBody } from "fumadocs-ui/page";
import { notFound } from "next/navigation";
import defaultMdxComponents from "fumadocs-ui/mdx";
import type { Metadata } from "next";
import { DocsHero } from "@/components/hero";
import { Byline, NumberedCards, NumberedCard, NumberedSteps, Step } from "@/components/editorial";
import { i18n, type Lang } from "@/lib/i18n";
import { homeCopy } from "@/lib/translations";
import { docsAlternates } from "@/lib/site";
import { DocsLocaleProvider, LocaleLink } from "@/components/locale-link";

function asLang(lang: string): Lang {
  return (i18n.languages as readonly string[]).includes(lang)
    ? (lang as Lang)
    : (i18n.defaultLanguage as Lang);
}

// A layout's `generateStaticParams` does NOT cascade — every page that
// wants SSG must declare its own. Without this, both `/docs/` and
// `/docs/zh` (the busiest URLs on the site) render dynamically on every
// request.
export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export default async function Page({
  params,
}: {
  params: Promise<{ lang: string }>;
}) {
  const { lang: rawLang } = await params;
  const lang = asLang(rawLang);
  const page = source.getPage([], lang);
  if (!page) notFound();

  const MDX = page.data.body;
  const copy = homeCopy[lang];

  return (
    <DocsPage toc={page.data.toc}>
      <DocsHero
        eyebrow={copy.eyebrow}
        title={
          <>
            {copy.titleLead}
            <em className="font-medium not-italic text-[var(--primary)]">
              {copy.titleAccent}
            </em>
          </>
        }
        subtitle={page.data.description}
      />
      <Byline items={[...copy.byline]} />
      <DocsBody>
        <DocsLocaleProvider lang={lang}>
          <MDX
            components={{
              ...defaultMdxComponents,
              a: LocaleLink,
              NumberedCards,
              NumberedCard,
              NumberedSteps,
              Step,
            }}
          />
        </DocsLocaleProvider>
      </DocsBody>
    </DocsPage>
  );
}

export async function generateMetadata({
  params,
}: {
  params: Promise<{ lang: string }>;
}): Promise<Metadata> {
  const { lang: rawLang } = await params;
  const lang = asLang(rawLang);
  const page = source.getPage([], lang);
  if (!page) notFound();

  return {
    title: page.data.title,
    description: page.data.description,
    alternates: docsAlternates([]),
  };
}
