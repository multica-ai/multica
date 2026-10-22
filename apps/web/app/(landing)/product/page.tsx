import type { Metadata } from "next";
import { DirectoryPage } from "@/features/landing/site/site-page";
import { PRODUCT_GROUPS, productHref } from "@/features/landing/site/sitemap";

export const metadata: Metadata = {
  title: "Product",
  alternates: { canonical: "/product" },
  robots: { index: false, follow: true },
};

export default function Page() {
  return (
    <DirectoryPage
      title="Product"
      lead="Placeholder: everything your human and agent team needs, in one workspace."
      groups={PRODUCT_GROUPS}
      href={productHref}
    />
  );
}
