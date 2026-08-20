"use client";

import { LandingHeader } from "@/features/landing/components/landing-header";
import { LandingFooter } from "@/features/landing/components/landing-footer";
import { CliSection } from "@/features/landing/components/download/cli-section";

export function DownloadClient() {
  return (
    <>
      <div className="relative bg-[#0a0d12] pb-20 pt-32 text-white sm:pb-24 sm:pt-36">
        <LandingHeader variant="dark" />
        <div className="mx-auto max-w-[920px] px-4 sm:px-6 lg:px-8">
          <h1 className="text-display-xl font-semibold tracking-tight">
            Install Multica CLI
          </h1>
          <p className="mt-4 max-w-2xl text-body-lg text-white/65">
            Multica runs in your browser. Install the CLI when you want to
            connect local or remote agent runtimes.
          </p>
        </div>
      </div>
      <CliSection />
      <LandingFooter />
    </>
  );
}
