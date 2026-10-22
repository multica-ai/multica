import type { Metadata } from "next";
import { FeaturesPreview } from "@/features/landing/site/features-preview";

export const metadata: Metadata = {
  title: "Features preview",
  robots: { index: false, follow: false },
};

export default function FeaturesPreviewPage() {
  return <FeaturesPreview />;
}
