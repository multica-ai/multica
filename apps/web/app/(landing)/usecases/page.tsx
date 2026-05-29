import type { Metadata } from "next";
import Link from "next/link";
import Image from "next/image";
import { LandingHeader } from "@/features/landing/components/landing-header";
import { LandingFooter } from "@/features/landing/components/landing-footer";
import {
  getUseCaseLangForLocale,
  useCasesSource,
} from "@/lib/use-cases-source";
import { getUseCaseLocale, useCaseText } from "@/lib/use-cases-i18n";

type ExtraFrontmatter = {
  category?: string;
  updated_at?: string;
  hero_image?: string;
};

export async function generateMetadata(): Promise<Metadata> {
  const locale = await getUseCaseLocale();
  const text = useCaseText[locale];
  return {
    title: text.indexMetadataTitle,
    description: text.indexMetadataDescription,
    openGraph: {
      title: text.indexMetadataTitle,
      description: text.indexMetadataDescription,
      url: "/usecases",
    },
    alternates: {
      canonical: "/usecases",
    },
  };
}

export default async function UseCasesIndexPage() {
  const locale = await getUseCaseLocale();
  const text = useCaseText[locale];
  const pages = useCasesSource
    .getPages(getUseCaseLangForLocale(locale))
    .slice()
    .sort((a, b) => {
      // Sort descending by updated_at. Missing dates fall to the bottom by
      // comparing against the epoch placeholder rather than the empty string
      // (which would make ordering undefined when multiple entries omit it).
      const da = (a.data as ExtraFrontmatter).updated_at ?? "1900-01-01";
      const db = (b.data as ExtraFrontmatter).updated_at ?? "1900-01-01";
      return db.localeCompare(da);
    });

  return (
    <>
      <div className="relative">
        <LandingHeader variant="dark" />
        <section className="relative overflow-hidden bg-[#05070b] text-white">
          <div className="relative z-10 mx-auto max-w-[1120px] px-4 pb-20 pt-32 text-center sm:px-6 sm:pt-40 lg:px-8 lg:pb-24">
            <h1 className="mx-auto max-w-[880px] font-[family-name:var(--font-serif)] text-[3rem] leading-[1.02] tracking-[-0.035em] drop-shadow-[0_10px_34px_rgba(0,0,0,0.32)] sm:text-[4rem] lg:text-[5rem]">
              {text.indexTitle}
            </h1>
            <p className="mx-auto mt-6 max-w-[620px] text-[15px] leading-7 text-white/84 sm:text-[17px]">
              {text.indexSubtitle}
            </p>
          </div>
        </section>
      </div>

      <section className="bg-white py-20 text-[#0a0d12] sm:py-24">
        <div className="mx-auto max-w-[1120px] px-4 sm:px-6 lg:px-8">
          <div className="grid grid-cols-1 gap-x-6 gap-y-14 sm:grid-cols-2 lg:grid-cols-3">
            {pages.map((page) => {
              const extra = page.data as ExtraFrontmatter;
              const heroImage = extra.hero_image;
              const category = extra.category;
              // Construct the URL from the page slug rather than `page.url`.
              // Fumadocs may prefix locale-specific URLs, but our public
              // route stays prefix-free and resolves locale from the request.
              const href = `/usecases/${page.slugs.join("/")}`;

              return (
                <Link key={href} href={href} className="group flex flex-col">
                  <div className="relative aspect-[4/3] overflow-hidden rounded-lg bg-[#f5f5f5]">
                    {heroImage ? (
                      <Image
                        src={heroImage}
                        alt=""
                        fill
                        sizes="(max-width: 640px) 100vw, (max-width: 1024px) 50vw, 360px"
                        className="object-cover transition-transform duration-300 group-hover:scale-[1.02]"
                      />
                    ) : null}
                  </div>
                  {category ? (
                    <div className="mt-5 text-[12px] uppercase tracking-[0.14em] text-[#0a0d12]/50">
                      {category}
                    </div>
                  ) : null}
                  <h3 className="mt-2 text-[18px] font-semibold leading-[1.35] text-[#0a0d12] sm:text-[20px]">
                    {page.data.title}
                  </h3>
                  {page.data.description ? (
                    <p className="mt-2 text-[14px] leading-[1.6] text-[#0a0d12]/60">
                      {page.data.description}
                    </p>
                  ) : null}
                  <span className="mt-3 text-[13px] text-[#0a0d12]/70 transition-colors group-hover:text-[#0a0d12]">
                    {text.cardReadMore}
                  </span>
                </Link>
              );
            })}
          </div>
        </div>
      </section>

      <LandingFooter />
    </>
  );
}
