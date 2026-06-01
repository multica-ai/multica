import type { Metadata } from "next";
import { fetchLatestRelease } from "@/features/landing/utils/github-release";
import { DownloadClient } from "./download-client";

// Render at request time, not at build. The release manifests are
// fetched from the in-cluster backend (REMOTE_API_URL), which isn't
// reachable during `next build`; a build-time prerender would bake an
// empty "version unavailable" page. The fetch itself still carries
// `next: { revalidate: 300 }`, so repeated requests are served from the
// fetch cache for 5 minutes.
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "Download Multica",
  description:
    "Download Multica for macOS, Windows, or Linux — or install the CLI for servers and remote dev boxes.",
  openGraph: {
    title: "Download Multica",
    description:
      "Get the Multica desktop app with a bundled daemon, or install the CLI for servers and remote dev boxes.",
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

export default async function DownloadPage() {
  const release = await fetchLatestRelease();
  return <DownloadClient release={release} />;
}
