import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { PrivacyPageClient } from "@/features/landing/components/privacy-page-client";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("privacy", "/privacy");
}

export default function PrivacyPage() {
  return <PrivacyPageClient />;
}
