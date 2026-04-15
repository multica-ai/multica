import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalPageClient } from "@/features/landing/components/legal-page-client";

type Props = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  return {
    title: locale === "de" ? "Impressum" : locale === "zh" ? "\u7f51\u7ad9\u6ce8\u518c" : "Imprint",
    description:
      locale === "de"
        ? "Rechtliche Impressum-Informationen f\u00fcr Multica."
        : locale === "zh"
          ? "Multica \u7684\u6cd5\u5f8b\u7f16\u793a\u4fe1\u606f\u3002"
          : "Legal imprint information for Multica.",
    alternates: {
      canonical: `/${locale}/imprint`,
    },
  };
}

export default async function ImprintPage({ params }: Props) {
  const { locale } = await params;

  if (locale !== "en" && locale !== "de" && locale !== "zh") {
    notFound();
  }

  return (
    <LegalPageClient
      title={
        locale === "de" ? "Impressum" : locale === "zh" ? "\u7f51\u7ad9\u6ce8\u518c" : "Imprint"
      }
      paragraphs={
        locale === "de"
          ? [
              "Multica ist ein Produkt der Multica Inc., einem in den Vereinigten Staaten registrierten Unternehmen.",
              "F\u00fcr rechtliche Anfragen kontaktieren Sie uns bitte unter legal@multica.ai.",
              "Diese Website wird von Multica Inc. betrieben und unterliegt den Gesetzen der Vereinigten Staaten.",
            ]
          : locale === "zh"
            ? [
                "Multica \u662f Multica Inc.\u7684\u4ea7\u54c1\uff0c\u4e00\u5bb6\u5728\u7f8e\u56fd\u6ce8\u518c\u7684\u516c\u53f8\u3002",
                "\u5982\u6709\u4efb\u4f55\u6cd5\u5f8b\u54a8\u8be2\uff0c\u8bf7\u901a\u8fc7 legal@multica.ai \u8054\u7cfb\u6211\u4eec\u3002",
                "\u672c\u7f51\u7ad9\u7531 Multica Inc. \u8fd0\u8425\uff0c\u5e94\u7b26\u5408\u7f8e\u56fd\u6cd5\u5f8b\u8981\u6c42\u3002",
              ]
            : [
                "Multica is a product of Multica Inc., a company registered in the United States.",
                "For any legal inquiries, please contact us at legal@multica.ai.",
                "This website is operated by Multica Inc. and is subject to the laws of the United States.",
              ]
      }
    />
  );
}
