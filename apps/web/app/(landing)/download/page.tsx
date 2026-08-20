import type { Metadata } from "next";
import { DownloadClient } from "./download-client";

export const metadata: Metadata = {
  title: "Install Multica",
  description:
    "Use Multica in the browser, or install the CLI for local and remote runtimes.",
  openGraph: {
    title: "Install Multica",
    description:
      "Use Multica in the browser, or install the CLI for local and remote runtimes.",
    url: "/download",
  },
  alternates: {
    canonical: "/download",
  },
};

export default function DownloadPage() {
  return <DownloadClient />;
}
