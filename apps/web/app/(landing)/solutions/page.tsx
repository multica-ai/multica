import type { Metadata } from "next";
import { DirectoryPage } from "@/features/landing/site/site-page";
import { SOLUTION_GROUPS, solutionHref, liveMetadata, requireLiveRoute } from "@/features/landing/site/sitemap";

export const metadata: Metadata = liveMetadata("/solutions", {
  title: "Solutions",
  alternates: { canonical: "/solutions" },
  robots: { index: false, follow: true },
});

export default function Page() {
  requireLiveRoute("/solutions");
  return (
    <DirectoryPage
      title="Solutions"
      lead="Placeholder: how teams put Multica to work."
      groups={SOLUTION_GROUPS}
      href={solutionHref}
    />
  );
}
