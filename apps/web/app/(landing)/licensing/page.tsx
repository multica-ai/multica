import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { LicensingPageClient } from "@/features/landing/components/licensing-page-client";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("licensing", "/licensing");
}

export default function LicensingPage() {
  return <LicensingPageClient />;
}
