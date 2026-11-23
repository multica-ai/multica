import { parseInlineLinks } from "./components/inline-links";
import { discordUrl, githubUrl, twitterUrl } from "./components/shared";
import type { LandingDict } from "./i18n/types";

export const SITE_ORIGIN = "https://www.multica.ai";

/** Organization and product facts shared by every marketing page. */
export const siteJsonLd = {
  "@context": "https://schema.org",
  "@graph": [
    {
      "@type": "Organization",
      name: "Multica",
      url: SITE_ORIGIN,
      sameAs: [githubUrl, twitterUrl, discordUrl],
    },
    {
      "@type": "SoftwareApplication",
      name: "Multica",
      applicationCategory: "ProjectManagement",
      operatingSystem: "Web, macOS, Windows, Linux, iOS",
      description:
        "Source-available project management platform that turns coding agents into real teammates.",
      offers: {
        "@type": "Offer",
        price: "0",
        priceCurrency: "USD",
      },
    },
  ],
};

/** Dictionary copy with its `[label](href)` links reduced to their labels. */
export function plainInlineText(text: string): string {
  return parseInlineLinks(text)
    .map((segment) => (segment.type === "text" ? segment.text : segment.label))
    .join("");
}

/** The home page FAQ, from the same dictionary entries the page renders. */
export function faqPageJsonLd(faq: LandingDict["faq"]) {
  return {
    "@context": "https://schema.org",
    "@type": "FAQPage",
    mainEntity: faq.items.map((item) => ({
      "@type": "Question",
      name: item.question,
      acceptedAnswer: { "@type": "Answer", text: plainInlineText(item.answer) },
    })),
  };
}

/** JSON for an inline `<script type="application/ld+json">`; `<` is escaped so copy cannot close the tag. */
export function jsonLdScript(data: unknown): string {
  return JSON.stringify(data).replace(/</g, "\\u003c");
}
