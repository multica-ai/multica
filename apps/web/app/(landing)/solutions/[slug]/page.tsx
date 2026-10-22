import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { SOLUTION_GROUPS, findPage, solutionHref } from "@/features/landing/site/sitemap";

type Params = { slug: string };

export const dynamicParams = false;

export function generateStaticParams(): Params[] {
  return SOLUTION_GROUPS.flatMap((group) => group.pages.map((page) => ({ slug: page.slug })));
}

export async function generateMetadata(props: { params: Promise<Params> }): Promise<Metadata> {
  const { slug } = await props.params;
  const page = findPage(SOLUTION_GROUPS, slug);
  if (!page) return {};
  return {
    title: page.title,
    description: page.summary,
    alternates: { canonical: solutionHref(page.slug) },
    robots: { index: false, follow: true },
  };
}

export default async function Page(props: { params: Promise<Params> }) {
  const { slug } = await props.params;
  const group = SOLUTION_GROUPS.find((g) => g.pages.some((page) => page.slug === slug));
  const page = findPage(SOLUTION_GROUPS, slug);
  if (!group || !page) notFound();
  return <PlaceholderPage eyebrow={group.title} page={page} />;
}
