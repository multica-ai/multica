import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { DownloadClient } from "./download-client";

vi.mock("next/link", () => ({
  default: ({
    children,
    href,
  }: {
    children: ReactNode;
    href: string;
  }) => <a href={href}>{children}</a>,
}));

vi.mock("@/features/landing/components/landing-header", () => ({
  LandingHeader: () => <div>landing-header</div>,
}));

vi.mock("@/features/landing/components/landing-footer", () => ({
  LandingFooter: () => <div>landing-footer</div>,
}));

vi.mock("@/features/landing/components/download/hero", () => ({
  DownloadHero: () => <div>download-hero</div>,
}));

vi.mock("@/features/landing/components/download/all-platforms", () => ({
  AllPlatforms: () => <div>all-platforms</div>,
}));

vi.mock("@/features/landing/components/download/cli-section", () => ({
  CliSection: () => <div>cli-section</div>,
}));

vi.mock("@/features/landing/components/download/cloud-section", () => ({
  CloudSection: () => <div>cloud-section-marker</div>,
}));

vi.mock("@/features/landing/i18n", () => ({
  useLocale: () => ({
    t: {
      download: {
        footer: {
          currentVersion: "Current version: {version}",
          releaseNotes: "{version} release notes",
          versionUnavailable: "Version unavailable",
          allReleases: "See all releases",
        },
      },
    },
  }),
}));

vi.mock("@/features/landing/utils/os-detect", () => ({
  detectOS: vi.fn(() => new Promise(() => {})),
}));

vi.mock("@multica/core/analytics", () => ({
  captureDownloadPageViewed: vi.fn(),
}));

describe("DownloadClient", () => {
  it("does not render the cloud runtime section on the download page", () => {
    render(
      <DownloadClient
        release={{
          version: "0.2.26",
          publishedAt: "2026-05-04T00:00:00.000Z",
          htmlUrl: "https://example.com/release",
          assets: {},
        }}
      />,
    );

    expect(screen.queryByText("cloud-section-marker")).not.toBeInTheDocument();
  });
});
