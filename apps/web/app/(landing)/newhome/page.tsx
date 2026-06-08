import type { Metadata } from "next";
import { NewHomeLanding } from "@/features/landing/newhome/newhome-landing";

export const metadata: Metadata = {
  title: "Multica — Landing V2 (sandbox)",
  description: "Work-in-progress rebuild of the Multica landing page.",
  robots: { index: false, follow: false },
  alternates: {
    canonical: "/newhome",
  },
};

export default function NewHomePage() {
  return <NewHomeLanding />;
}
