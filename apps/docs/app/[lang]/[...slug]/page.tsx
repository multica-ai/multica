import { source } from "@/lib/source";
import {
  DocsPage,
  DocsBody,
  DocsDescription,
  DocsTitle,
} from "fumadocs-ui/page";
import { notFound } from "next/navigation";
import defaultMdxComponents from "fumadocs-ui/mdx";
import { ImageZoom } from "fumadocs-ui/components/image-zoom";
import type { Metadata } from "next";
import { docsAlternates } from "@/lib/site";
import { i18n, type Lang } from "@/lib/i18n";
import { DocsLocaleProvider, LocaleLink } from "@/components/locale-link";
import { VideoEmbed } from "@/components/video-embed";
import { docsSlugStaticParams } from "@/lib/static-params";

function asLang(lang: string): Lang {
  return (i18n.languages as readonly string[]).includes(lang)
    ? (lang as Lang)
    : (i18n.defaultLanguage as Lang);
}

export default async function Page(props: {
  params: Promise<{ lang: string; slug: string[] }>;
}) {
  const params = await props.params;
  const lang = asLang(params.lang);
  const page = source.getPage(params.slug, lang);
  if (!page) notFound();

  const MDX = page.data.body;

  return (
    <DocsPage toc={page.data.toc}>
      <DocsTitle>{page.data.title}</DocsTitle>
      <DocsDescription className="mb-0">{page.data.description}</DocsDescription>
      <DocsBody>
        <DocsLocaleProvider lang={lang}>
          <MDX
            components={{
              ...defaultMdxComponents,
              // Every markdown image gets the standard Fumadocs lightbox:
              // click to zoom to viewport, scroll/Esc/click to dismiss.
              img: (props) => <ImageZoom {...props} />,
              a: LocaleLink,
              VideoEmbed,
            }}
          />
        </DocsLocaleProvider>
      </DocsBody>
    </DocsPage>
  );
}

export function generateStaticParams() {
  return docsSlugStaticParams(source.generateParams());
}

export async function generateMetadata(props: {
  params: Promise<{ lang: string; slug: string[] }>;
}): Promise<Metadata> {
  const params = await props.params;
  const lang = asLang(params.lang);
  const page = source.getPage(params.slug, lang);
  if (!page) notFound();

  return {
    title: page.data.title,
    description: page.data.description,
    alternates: docsAlternates(params.slug),
  };
}
