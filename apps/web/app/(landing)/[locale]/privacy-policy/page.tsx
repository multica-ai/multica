import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalPageClient } from "@/features/landing/components/legal-page-client";

type Props = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  return {
    title: locale === "de" ? "Datenschutz" : locale === "zh" ? "\u9690\u79c1\u653f\u7b56" : "Privacy Policy",
    description:
      locale === "de"
        ? "Datenschutzrichtlinie f\u00fcr Multica."
        : locale === "zh"
          ? "Multica \u7684\u9690\u79c1\u653f\u7b56\u3002"
          : "Privacy policy for Multica.",
    alternates: {
      canonical: `/${locale}/privacy-policy`,
    },
  };
}

export default async function PrivacyPolicyPage({ params }: Props) {
  const { locale } = await params;

  if (locale !== "en" && locale !== "de" && locale !== "zh") {
    notFound();
  }

  return (
    <LegalPageClient
      title={
        locale === "de" ? "Datenschutz" : locale === "zh" ? "\u9690\u79c1\u653f\u7b56" : "Privacy Policy"
      }
      paragraphs={
        locale === "de"
          ? [
              "Multica Inc. (\"wir\", \"uns\" oder \"unser\") betreibt die Multica-Plattform als SaaS-Produkt.",
              "Diese Datenschutzrichtlinie informiert Sie \u00fcber unsere Praktiken bez\u00fcglich der Erfassung, Verwendung und Offenlegung von Informationen, die wir von Benutzern unserer Plattform erhalten.",
              "Wir erfassen verschiedene Arten von Informationen, um unsere Dienste bereitzustellen und zu verbessern, einschlie\u00dflich personenbezogener Daten, die Sie uns direkt zur Verf\u00fcgung stellen, sowie automatisch erfasste Informationen \u00fcber Ihre Nutzung unserer Dienste.",
              "Wir geben Ihre personenbezogenen Daten nicht an Dritte weiter, au\u00dfer in den in dieser Richtlinie beschriebenen F\u00e4llen oder mit Ihrer ausdr\u00fccklichen Einwilligung.",
            ]
          : locale === "zh"
            ? [
                "Multica Inc.(\"\u6211\u4eec\"\u6216\"\u4f60\u4eec\")\u8fd0\u8425 Multica \u5e73\u53f0\u4f5c\u4e3a SaaS \u4ea7\u54c1\u3002",
                "\u672c\u9690\u79c1\u653f\u7b56\u5c06\u544a\u8bc9\u60a8\u5173\u4e8e\u6211\u4eec\u5728\u64cd\u4f5c\u5e73\u53f0\u65f6\u6536\u96c6\u3001\u4f7f\u7528\u548c\u6b63\u5f0f\u5411\u60a8\u6aa2\u7d27\u7684\u4fe1\u606f\u7684\u505a\u6cd5\u3002",
                "\u6211\u4eec\u6536\u96c6\u591a\u79cd\u7c7b\u578b\u7684\u4fe1\u606f\uff0c\u4ee5\u63d0\u4f9b\u548c\u6539\u5584\u6211\u4eec\u7684\u670d\u52a1\uff0c\u5305\u62ec\u60a8\u76f4\u63a5\u63d0\u4f9b\u7ed9\u6211\u4eec\u7684\u4e2a\u4eba\u4fe1\u606f\u548c\u81ea\u52a8\u6536\u96c6\u7684\u5173\u4e8e\u60a8\u4f7f\u7528\u6211\u4eec\u670d\u52a1\u7684\u4fe1\u606f\u3002",
                "\u9664\u975e\u672c\u653f\u7b56\u4e2d\u63cf\u8ff0\u7684\u60c5\u51b5\u6216\u5f97\u5230\u60a8\u7684\u660e\u793a\u540c\u610f\uff0c\u6211\u4eec\u4e0d\u4f1a\u5c06\u60a8\u7684\u4e2a\u4eba\u4fe1\u606f\u5171\u4eab\u7ed9\u7b2c\u4e09\u65b9\u3002",
              ]
            : [
                "Multica Inc. (\"we\", \"us\", or \"our\") operates the Multica platform as a SaaS product.",
                "This Privacy Policy informs you about our practices regarding the collection, use, and disclosure of information that we receive from users of our platform.",
                "We collect various types of information to provide and improve our services, including personal information you directly provide to us, and automatically collected information about your use of our services.",
                "We do not share your personal information with third parties except in the circumstances described in this policy or with your explicit consent.",
              ]
      }
    />
  );
}
