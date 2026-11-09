import type { Metadata } from "next";
import { pageMetadata } from "@/features/landing/utils/page-metadata";
import { ContactSalesPageClient } from "@/features/landing/components/contact-sales-page-client";

export function generateMetadata(): Promise<Metadata> {
  return pageMetadata("contactSales", "/contact-sales");
}

export default function ContactSalesPage() {
  return <ContactSalesPageClient />;
}
