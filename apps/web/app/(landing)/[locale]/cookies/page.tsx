import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { LegalPageClient } from "@/features/landing/components/legal-page-client";

type Props = {
  params: Promise<{ locale: string }>;
};

export async function generateMetadata({ params }: Props): Promise<Metadata> {
  const { locale } = await params;
  return {
    title: locale === "de" ? "Cookie-Richtlinie" : locale === "zh" ? "Cookie \u653f\u7b56" : "Cookie Policy",
    description:
      locale === "de"
        ? "Cookie-Richtlinie f\u00fcr Multica."
        : locale === "zh"
          ? "Multica \u7684 Cookie \u653f\u7b56\u3002"
          : "Cookie policy for Multica.",
    alternates: {
      canonical: `/${locale}/cookies`,
    },
  };
}

export default async function CookiesPage({ params }: Props) {
  const { locale } = await params;

  if (locale !== "en" && locale !== "de" && locale !== "zh") {
    notFound();
  }

  return (
    <LegalPageClient
      title={
        locale === "de" ? "Cookie-Richtlinie" : locale === "zh" ? "Cookie \u653f\u7b56" : "Cookie Policy"
      }
      paragraphs={
        locale === "de"
          ? [
              "Wir verwenden Cookies und \u00e4hnliche Technologien, um unsere Dienste bereitzustellen und zu verbessern.",
              "Cookies sind kleine Textdateien, die von Websites auf Ihrem Ger\u00e4t gespeichert werden, um Informationen \u00fcber Ihre Nutzung zu speichern.",
              "Wir verwenden essenzielle Cookies, die f\u00fcr den Betrieb der Plattform erforderlich sind, sowie optionale Cookies f\u00fcr Analytics und Benutzerpr\u00e4ferenzen.",
              "Sie k\u00f6nnen Ihre Cookie-Pr\u00e4ferenzen in Ihren Browsereinstellungen verwalten oder durch L\u00f6schung Ihrer Browser-Cookies.",
            ]
          : locale === "zh"
            ? [
                "\u6211\u4eec\u4f7f\u7528 Cookie \u548c\u76f8\u5173\u6280\u672f\u6765\u63d0\u4f9b\u548c\u6539\u5584\u6211\u4eec\u7684\u670d\u52a1\u3002",
                "Cookie \u662f\u7531\u7f51\u7ad9\u50a8\u5b58\u5728\u60a8\u8bbe\u5907\u4e0a\u7684\u5c0f\u6587\u672c\u6587\u4ef6\uff0c\u7528\u4e8e\u50a8\u5b58\u6709\u5173\u60a8\u4f7f\u7528\u60c5\u51b5\u7684\u4fe1\u606f\u3002",
                "\u6211\u4eec\u4f7f\u7528\u5fc5\u8981\u7684 Cookie\uff08\u5bf9\u4e8e\u5e73\u53f0\u8fd0\u884c\u6240\u9700\uff09\u548c\u53ef\u9009\u7684 Cookie\uff08\u7528\u4e8e\u5206\u6790\u548c\u7528\u6237\u9996\u9009\u9879\u3002",
                "\u60a8\u53ef\u4ee5\u5728\u6d4f\u89c8\u5668\u8bbe\u7f6e\u4e2d\u7ba1\u7406\u60a8\u7684 Cookie \u9996\u9009\u9879\uff0c\u6216\u901a\u8fc7\u5220\u9664\u6d4f\u89c8\u5668 Cookie \u6765\u64a4\u9500\u540c\u610f\u3002",
              ]
            : [
                "We use cookies and similar technologies to provide and improve our services.",
                "Cookies are small text files stored on your device by websites to remember information about your usage.",
                "We use essential cookies that are required for the platform to operate, and optional cookies for analytics and user preferences.",
                "You can manage your cookie preferences in your browser settings or by deleting your browser cookies.",
              ]
      }
    />
  );
}
