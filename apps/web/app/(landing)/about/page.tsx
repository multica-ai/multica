import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { AboutPageClient } from "@/features/landing/components/about-page-client";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("about", "/about");
}

export default function AboutPage() {
  return <AboutPageClient />;
}
