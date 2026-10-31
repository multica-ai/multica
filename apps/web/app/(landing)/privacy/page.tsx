import type { Metadata } from "next";
import { PrivacyPageClient } from "@/features/landing/components/privacy-page-client";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: "Privacy Policy",
  description:
    "How Multica collects, uses, and protects personal information on multica.ai and Multica Cloud.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Privacy Policy — Multica",
    description:
      "How Multica collects, uses, and protects personal information.",
    url: "/privacy",
  },
  alternates: {
    canonical: "/privacy",
  },
};

export default function PrivacyPage() {
  return <PrivacyPageClient />;
}
