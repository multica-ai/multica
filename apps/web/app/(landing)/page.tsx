import type { Metadata } from "next";
import { RootRedirect } from "@/features/landing/components/root-redirect";

// FIR-2490 follow-up: this fork is an internal tool — the root URL is
// never a marketing page. The proxy redirects anon → /login and known
// users → their last workspace server-side. RootRedirect only handles
// the rare case where a user is authenticated but has no
// last_workspace_slug cookie yet (first login since the migration).
export const metadata: Metadata = {
  title: {
    absolute: "Multica",
  },
  robots: {
    index: false,
    follow: false,
  },
};

export default function LandingPage() {
  return <RootRedirect />;
}
