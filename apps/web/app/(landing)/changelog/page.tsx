import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { ChangelogPageClient } from "@/features/landing/components/changelog-page-client";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("changelog", "/changelog");
}

export default function ChangelogPage() {
  return <ChangelogPageClient />;
}
