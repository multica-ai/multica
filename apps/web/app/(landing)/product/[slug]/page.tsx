import type { Metadata } from "next";
import { notFound } from "next/navigation";
import type { ComponentType } from "react";
import { ChatPage } from "@/features/landing/site/chat-page";
import { CliPage } from "@/features/landing/site/cli-page";
import { CloudPage } from "@/features/landing/site/cloud-page";
import { DesktopPage } from "@/features/landing/site/desktop-page";
import { MobilePage } from "@/features/landing/site/mobile-page";
import { PlaceholderPage } from "@/features/landing/site/site-page";
import { PRODUCT_GROUPS, findPage, productHref, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

type Params = { slug: string };

const BUILT_PAGES: Record<string, ComponentType> = {
  cloud: CloudPage,
  desktop: DesktopPage,
  cli: CliPage,
  chat: ChatPage,
  mobile: MobilePage,
};

export const dynamicParams = false;

export function generateStaticParams(): Params[] {
  return PRODUCT_GROUPS.flatMap((group) => group.pages.map((page) => ({ slug: page.slug })));
}

export async function generateMetadata(props: { params: Promise<Params> }): Promise<Metadata> {
  const { slug } = await props.params;
  const page = findPage(PRODUCT_GROUPS, slug);
  if (!page) return {};
  return liveMetadata(productHref(page.slug), {
    title: page.title,
    description: page.summary,
    alternates: { canonical: productHref(page.slug) },
    robots: { index: false, follow: true },
  });
}

export default async function Page(props: { params: Promise<Params> }) {
  const { slug } = await props.params;
  const group = PRODUCT_GROUPS.find((g) => g.pages.some((page) => page.slug === slug));
  const page = findPage(PRODUCT_GROUPS, slug);
  if (!group || !page) notFound();
  requireLiveRoute(productHref(page.slug));
  const Built = BUILT_PAGES[page.slug];
  if (Built) return <Built />;
  return <PlaceholderPage eyebrow={group.title} page={page} />;
}
