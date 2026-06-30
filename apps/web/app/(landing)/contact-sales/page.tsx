import type { Metadata } from "next";
import { ContactSalesPageClient } from "@/features/landing/components/contact-sales-page-client";
import { WEB_BRAND_NAME } from "@/lib/brand";

export const metadata: Metadata = {
  title: "Contact Sales",
  description:
    `Talk to the ${WEB_BRAND_NAME} team about rolling out human + agent workflows at your company.`,
  openGraph: {
    title: `Contact Sales — ${WEB_BRAND_NAME}`,
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
