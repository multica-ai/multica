import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalPageClient } from "@/features/landing/components/legal-page-client";

type Props = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  return {
    title: locale === "de" ? "AGB" : locale === "zh" ? "\u670d\u52a1\u6761\u6b3e" : "Terms of Service",
    description:
      locale === "de"
        ? " Allgemeine Gesch\u00e4ftsbedingungen f\u00fcr Multica."
        : locale === "zh"
          ? "Multica \u7684\u670d\u52a1\u6761\u6b3e\u3002"
          : "Terms of service for Multica.",
    alternates: {
      canonical: `/${locale}/terms`,
    },
  };
}

export default async function TermsPage({ params }: Props) {
  const { locale } = await params;

  if (locale !== "en" && locale !== "de" && locale !== "zh") {
    notFound();
  }

  return (
    <LegalPageClient
      title={
        locale === "de" ? "AGB" : locale === "zh" ? "\u670d\u52a1\u6761\u6b3e" : "Terms of Service"
      }
      paragraphs={
        locale === "de"
          ? [
              "Durch die Nutzung der Multica-Plattform erkl\u00e4ren Sie sich mit diesen Allgemeinen Gesch\u00e4ftsbedingungen (\"AGB\") einverstanden.",
              "Multica Inc. (\"Multica\") bietet die Multica-Plattform als SaaS-Produkt an. Die Nutzung der Plattform unterliegt diesen AGB sowie allen geltenden Gesetzen und Vorschriften.",
              "Sie sind daf\u00fcr verantwortlich, Ihr Konto sicher zu halten und alle Aktivit\u00e4ten zu verantworten, die unter Ihrem Konto stattfinden.",
              "Wir behalten uns das Recht vor, diese AGB jederzeit zu \u00e4ndern. Fortgesetzte Nutzung der Plattform nach \u00c4nderungen gilt als Annahme der ge\u00e4nderten Bedingungen.",
            ]
          : locale === "zh"
            ? [
                "\u901a\u8fc7\u4f7f\u7528 Multica \u5e73\u53f0\uff0c\u60a8\u8868\u793a\u60a8\u540c\u610f\u672c\u670d\u52a1\u6761\u6b3e (\"ToS\")\u3002",
                "Multica Inc. (\"Multica\") \u4f5c\u4e3a SaaS \u4ea7\u54c1\u63d0\u4f9b Multica \u5e73\u53f0\u3002\u5e73\u53f0\u7684\u4f7f\u7528\u53d7\u672c ToS \u548c\u6240\u6709\u9002\u7528\u7684\u6cd5\u5f8b\u6cd5\u89c4\u7684\u7ea6\u675f\u3002",
                "\u60a8\u8d1f\u8d23\u4fdd\u6301\u60a8\u7684\u8d26\u6237\u5b89\u5168\uff0c\u5e76\u5bf9\u60a8\u8d26\u6237\u4e0b\u53d1\u751f\u7684\u6240\u6709\u6d3b\u52a8\u8d1f\u8d23\u3002",
                "\u6211\u4eec\u4fdd\u7559\u968f\u65f6\u66f4\u6539\u672c ToS \u7684\u6743\u5229\u3002\u66f4\u6539\u540e\u7ee7\u7eed\u4f7f\u7528\u5e73\u53f0\u89ba\u5f97\u60a8\u63a5\u53d7\u66f4\u6539\u540e\u7684\u6761\u6b3e\u3002",
              ]
            : [
                "By using the Multica platform, you agree to be bound by these Terms of Service (\"ToS\").",
                "Multica Inc. (\"Multica\") offers the Multica platform as a SaaS product. Use of the platform is subject to these ToS and all applicable laws and regulations.",
                "You are responsible for keeping your account secure and for all activities that occur under your account.",
                "We reserve the right to modify these ToS at any time. Continued use of the platform after changes constitutes acceptance of the modified terms.",
              ]
      }
    />
  );
}
