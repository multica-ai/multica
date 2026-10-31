import type { Metadata } from "next";
import { ContactSalesPageClient } from "@/features/landing/components/contact-sales-page-client";
import { SHARE_IMAGE } from "@/features/landing/share-image";

export const metadata: Metadata = {
  title: "Contact Sales",
  description:
    "Talk to the Multica team about rolling out human + agent workflows at your company.",
  openGraph: {
    images: [SHARE_IMAGE],
    title: "Contact Sales — Multica",
    description:
      "Tell us about your team. We’ll respond within three business days.",
    url: "/contact-sales",
  },
  alternates: {
    canonical: "/contact-sales",
  },
};

export default function ContactSalesPage() {
  return <ContactSalesPageClient />;
}
