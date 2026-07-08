import type { Metadata } from "next";
import type { ComponentPropsWithoutRef, ReactNode } from "react";
import Link from "next/link";
import { notFound } from "next/navigation";
import { cn } from "@multica/ui/lib/utils";
import { LandingHeader } from "@/features/landing/components/landing-header";
import { LandingFooter } from "@/features/landing/components/landing-footer";
import { Screenshot } from "@/features/landing/components/mdx/screenshot";
import { getUseCasePageForLocale } from "@/lib/use-cases-source";
import {
  docsHrefForLocale,
  getUseCaseLocale,
  useCaseText,
} from "@/lib/use-cases-i18n";
import type { SupportedLocale } from "@multica/core/i18n";

type Params = { slug: string };

type TocItem = { title: ReactNode; url: string; depth: number };

export async function generateMetadata(props: {
  params: Promise<Params>;
}): Promise<Metadata> {
  const { slug } = await props.params;
  const locale = await getUseCaseLocale();
  const page = getUseCasePageForLocale([slug], locale);
  if (!page) return {};

  return {
    title: page.data.title,
    description: page.data.description,
    openGraph: {
      title: page.data.title,
      description: page.data.description,
      url: `/usecases/${slug}`,
    },
    alternates: {
      canonical: `/usecases/${slug}`,
    },
  };
}

function nodeToString(node: ReactNode): string {
  if (typeof node === "string") return node;
  if (typeof node === "number") return String(node);
  if (Array.isArray(node)) return node.map(nodeToString).join("");
  return "";
}

function PlaceholderImage({ label }: { label: string }) {
  return (
    <figure className="my-10 -mx-4 sm:mx-0">
      <div
        className={cn(
          "flex aspect-[16/9] items-center justify-center rounded-lg",
          "border-2 border-dashed border-[#0a0d12]/15 bg-[#fafafa]",
          "px-6 text-center text-[13px] italic leading-relaxed text-[#0a0d12]/55",
        )}
      >
        {label}
      </div>
    </figure>
  );
}

function MDXCTA({
  label,
  variant,
  href,
}: {
  label: string;
  variant: "primary" | "secondary";
  href: string;
}) {
  return (
    <Link
      href={href}
      className={cn(
        "inline-flex items-center gap-2 rounded-[12px] px-5 py-3 text-[14px] font-semibold not-italic transition-colors",
        variant === "primary"
          ? "bg-[#0a0d12] text-white hover:bg-[#0a0d12]/88"
          : "border border-[#0a0d12]/15 text-[#0a0d12] hover:bg-[#0a0d12]/[0.04]",
      )}
    >
      {label}
    </Link>
  );
}

// Marker patterns accept both half-width (`:`) and full-width (`：`) colons so
// content authors can type either without breaking the placeholder rendering.
// `副 ` is the Chinese marker for the secondary CTA and `Secondary ` is the
// English equivalent — either works in either language's mdx file.
const PLACEHOLDER_IMAGE_PREFIX = /^\[占位图[::]\s*/;
const CTA_SECONDARY_PREFIX = "(?:副 |Secondary )";
const CTA_BLOCK_TRIGGER = new RegExp(`^\\[${CTA_SECONDARY_PREFIX}?CTA[::]`);
const CTA_LABEL_STRIP = new RegExp(`^${CTA_SECONDARY_PREFIX}?CTA[::]\\s*`);
const CTA_PRIMARY = /^CTA[::]/;

function createSmartParagraph(locale: SupportedLocale) {
  const secondaryHref = docsHrefForLocale(locale);
  return function SmartParagraph(props: ComponentPropsWithoutRef<"p">) {
    const text = nodeToString(props.children).trim();

    if (text.startsWith("[占位图")) {
      const label = text
        .replace(PLACEHOLDER_IMAGE_PREFIX, "")
        .replace(/\]$/, "");
      return <PlaceholderImage label={label} />;
    }

    if (CTA_BLOCK_TRIGGER.test(text)) {
      const items = Array.from(text.matchAll(/\[([^\]]+)\]/g)).map(
        (m) => m[1]!,
      );
      return (
        <div className="my-8 flex flex-wrap items-center gap-3">
          {items.map((item, i) => {
            const isPrimary = CTA_PRIMARY.test(item);
            const label = item.replace(CTA_LABEL_STRIP, "");
            return (
              <MDXCTA
                key={i}
                label={label}
                variant={isPrimary ? "primary" : "secondary"}
                href={isPrimary ? "/" : secondaryHref}
              />
            );
          })}
        </div>
      );
    }

    return <p {...props} />;
  };
}

function createMdxComponents(locale: SupportedLocale) {
  const SmartParagraph = createSmartParagraph(locale);
  return {
    Screenshot,
    h2: (props: ComponentPropsWithoutRef<"h2">) => (
      <h2
        className="mt-16 mb-4 scroll-mt-[100px] text-[1.5rem] font-semibold tracking-tight text-[#0a0d12] sm:text-[1.75rem]"
        {...props}
      />
    ),
    h3: (props: ComponentPropsWithoutRef<"h3">) => (
      <h3
        className="mt-10 mb-3 scroll-mt-[100px] text-[1.1rem] font-semibold tracking-tight text-[#0a0d12] sm:text-[1.2rem]"
        {...props}
      />
    ),
    p: SmartParagraph,
    strong: (props: ComponentPropsWithoutRef<"strong">) => (
      <strong className="font-semibold text-[#0a0d12]" {...props} />
    ),
    hr: (props: ComponentPropsWithoutRef<"hr">) => (
      <hr className="my-12 border-[#0a0d12]/8" {...props} />
    ),
    blockquote: (props: ComponentPropsWithoutRef<"blockquote">) => (
      <blockquote
        className="my-6 border-l-2 border-[#0a0d12]/15 pl-5 text-[#0a0d12]/65 italic"
        {...props}
      />
    ),
    ul: (props: ComponentPropsWithoutRef<"ul">) => (
      <ul
        className="my-4 list-disc space-y-2 pl-6 marker:text-[#0a0d12]/30"
        {...props}
      />
    ),
    ol: (props: ComponentPropsWithoutRef<"ol">) => (
      <ol
        className="my-4 list-decimal space-y-2 pl-6 marker:text-[#0a0d12]/40"
        {...props}
      />
    ),
    li: (props: ComponentPropsWithoutRef<"li">) => (
      <li className="pl-1" {...props} />
    ),
    a: ({ href, ...props }: ComponentPropsWithoutRef<"a">) => {
      const className =
        "underline decoration-[#0a0d12]/25 underline-offset-4 transition-colors hover:text-[#0a0d12] hover:decoration-[#0a0d12]/70";
      // Internal links should keep SPA navigation (next/link); external links
      // (mailto:, https://, etc.) stay as native anchors.
      if (href && href.startsWith("/")) {
        return <Link href={href} className={className} {...props} />;
      }
      return <a href={href} className={className} {...props} />;
    },
    code: (props: ComponentPropsWithoutRef<"code">) => (
      <code
        className="rounded bg-[#0a0d12]/[0.06] px-1.5 py-0.5 font-mono text-[0.88em] text-[#0a0d12]"
        {...props}
      />
    ),
    pre: (props: ComponentPropsWithoutRef<"pre">) => (
      <pre
        className="my-6 overflow-x-auto rounded-lg bg-[#0a0d12]/[0.04] p-4 text-[13px] leading-[1.65]"
        {...props}
      />
    ),
  };
}

export default async function UseCasePage(props: { params: Promise<Params> }) {
  const { slug } = await props.params;
  const locale = await getUseCaseLocale();
  const text = useCaseText[locale];
  const page = getUseCasePageForLocale([slug], locale);
  if (!page) notFound();

  const MDX = page.data.body;
  const toc = ((page.data as { toc?: TocItem[] }).toc ?? []).filter(
    (item) => item.depth === 2 || item.depth === 3,
  );
  const mdxComponents = createMdxComponents(locale);

  return (
    <>
      <div className="sticky top-0 z-40 bg-white">
        <LandingHeader variant="light" />
      </div>
      <main className="bg-white text-[#0a0d12]">
        <div
          className={cn(
            "mx-auto max-w-[720px] px-4 py-16 sm:px-6 sm:py-20",
            "lg:max-w-[1100px] lg:py-24",
            "lg:grid lg:grid-cols-[minmax(0,720px)_220px] lg:gap-x-20",
          )}
        >
          <article>
            <h1 className="font-[family-name:var(--font-serif)] text-[2.6rem] leading-[1.05] tracking-[-0.03em] sm:text-[3.4rem]">
              {page.data.title}
            </h1>
            <div className="mt-10 text-[16px] leading-[1.85] text-[#0a0d12]/72 [&>:first-child]:mt-0 [&>p]:my-5 sm:text-[17px]">
              <MDX components={mdxComponents} />
            </div>
          </article>

          {toc.length > 0 ? (
            <aside className="hidden lg:block">
              <nav className="sticky top-[100px] max-h-[calc(100vh-120px)] overflow-y-auto">
                <div className="mb-3 text-[11px] font-medium uppercase tracking-[0.14em] text-[#0a0d12]/40">
                  {text.tableOfContents}
                </div>
                <ul className="border-l border-[#0a0d12]/8">
                  {toc.map((item, i) => (
                    <li key={i}>
                      <a
                        href={item.url}
                        className={cn(
                          "-ml-px block border-l border-transparent py-1.5 pl-4 text-[13px] leading-snug transition-colors",
                          "hover:border-[#0a0d12]/40 hover:text-[#0a0d12]",
                          item.depth === 2
                            ? "font-medium text-[#0a0d12]/70"
                            : "pl-7 text-[12px] text-[#0a0d12]/50",
                        )}
                      >
                        {item.title}
                      </a>
                    </li>
                  ))}
                </ul>
              </nav>
            </aside>
          ) : null}
        </div>
      </main>
      <LandingFooter />
    </>
  );
}
