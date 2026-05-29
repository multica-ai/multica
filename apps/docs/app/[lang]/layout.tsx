import "../global.css";
import { RootProvider } from "fumadocs-ui/provider";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import { Inter, Geist_Mono, Source_Serif_4 } from "next/font/google";
import type { ReactNode } from "react";
import type { Metadata } from "next";
import { cn } from "@multica/ui/lib/utils";
import { baseOptions } from "@/app/layout.config";
import { source } from "@/lib/source";
import { i18n, type Lang } from "@/lib/i18n";
import { uiTranslations, localeLabels } from "@/lib/translations";
import { DocsSettings } from "@/components/docs-settings";

const inter = Inter({
  subsets: ["latin"],
  variable: "--font-sans",
  fallback: [
    "-apple-system",
    "BlinkMacSystemFont",
    "Segoe UI",
    "PingFang SC",
    "Microsoft YaHei",
    "Noto Sans CJK SC",
    "Apple SD Gothic Neo",
    "Malgun Gothic",
    "Noto Sans CJK KR",
    "sans-serif",
  ],
});

const geistMono = Geist_Mono({
  subsets: ["latin"],
  variable: "--font-mono",
  fallback: ["ui-monospace", "SFMono-Regular", "Menlo", "Consolas", "monospace"],
});

// Japanese-scoped CJK font override. Japanese Kanji share the Han Unicode
// block with Chinese, so the global Chinese-first stack would render Kanji
// with Chinese glyph shapes; CSS font-fallback order is not affected by
// `<html lang>`. Scope a Japanese-first CJK chain to `<html lang="ja">` so
// Japanese docs render with Japanese glyphs while zh/en keep Chinese-first.
// Mirrors apps/web/app/layout.tsx. Inter still leads for Latin text.
const JA_FONT_FAMILY = [
  inter.style.fontFamily,
  '"Hiragino Sans"',
  '"Hiragino Kaku Gothic ProN"',
  '"Yu Gothic"',
  '"YuGothic"',
  '"Meiryo"',
  '"Noto Sans CJK JP"',
  '"Noto Sans JP"',
  "-apple-system",
  "BlinkMacSystemFont",
  '"Segoe UI"',
  '"PingFang SC"',
  '"Microsoft YaHei"',
  '"Noto Sans CJK SC"',
  '"Apple SD Gothic Neo"',
  '"Malgun Gothic"',
  '"Noto Sans CJK KR"',
  "sans-serif",
].join(", ");
// `[lang|="ja"]` is the BCP-47 language-range selector — matches exactly
// `lang="ja"` or `lang="ja-<region>"`, not unrelated subtags like `jam`.
const JA_FONT_OVERRIDE_CSS = `html[lang|="ja"]{--font-sans:${JA_FONT_FAMILY};}`;

// Editorial serif used for headings and showpiece elements. Italic style is
// deliberately NOT loaded — italic in CJK is a synthetic slant that breaks
// glyph design. Emphasis in docs is carried by brand color + weight, never
// font-style. Mirrors apps/web/app/layout.tsx for the upright family.
const sourceSerif = Source_Serif_4({
  subsets: ["latin"],
  style: ["normal"],
  variable: "--font-serif",
  fallback: [
    "ui-serif",
    "Iowan Old Style",
    "Apple Garamond",
    "Baskerville",
    "Times New Roman",
    "serif",
  ],
});

export const metadata: Metadata = {
  title: {
    template: "%s | Multica Docs",
    default: "Multica Docs",
  },
  description:
    "Documentation for Multica — the open-source managed agents platform.",
};

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export default async function Layout({
  params,
  children,
}: {
  params: Promise<{ lang: string }>;
  children: ReactNode;
}) {
  const { lang: rawLang } = await params;
  const lang = (i18n.languages as readonly string[]).includes(rawLang)
    ? (rawLang as Lang)
    : (i18n.defaultLanguage as Lang);
  const locales = i18n.languages.map((l) => ({
    locale: l,
    name: localeLabels[l],
  }));

  return (
    <html
      lang={lang}
      suppressHydrationWarning
      className={cn(
        "antialiased",
        inter.variable,
        geistMono.variable,
        sourceSerif.variable,
      )}
    >
      <body className="font-sans">
        <style dangerouslySetInnerHTML={{ __html: JA_FONT_OVERRIDE_CSS }} />
        <RootProvider
          i18n={{
            locale: lang,
            locales,
            translations: uiTranslations[lang],
          }}
          search={{ options: { api: "/docs/api/search" } }}
        >
          <DocsLayout
            tree={source.getPageTree(lang)}
            // Suppress Fumadocs's default sidebar-footer icons (theme +
            // language + search). Our custom <DocsSettings> is mounted as
            // the sidebar footer instead — two labelled buttons, not three
            // icons.
            themeSwitch={{ enabled: false }}
            searchToggle={{ enabled: false }}
            sidebar={{ footer: <DocsSettings locale={lang} /> }}
            {...baseOptions}
          >
            {children}
          </DocsLayout>
        </RootProvider>
      </body>
    </html>
  );
}
