import type { Metadata } from "next";

export const WEB_BRAND_NAME = "CoStrict";

export const WEB_BRAND_METADATA = {
  title: {
    default: `${WEB_BRAND_NAME} — Project Management for Human + Agent Teams`,
    template: `%s | ${WEB_BRAND_NAME}`,
  },
  description:
    "Open-source platform that turns coding agents into real teammates. Assign tasks, track progress, compound skills.",
  openGraph: {
    type: "website",
    siteName: WEB_BRAND_NAME,
    locale: "en_US",
  },
} satisfies Metadata;
