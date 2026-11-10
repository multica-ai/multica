import { fetchLatestRelease } from "./github-release";
import type { DetectResult } from "./os-detect";
import type { DownloadAssets, DownloadSizes } from "./parse-release-assets";
import { getRequestPlatform } from "./request-platform";

/** What the homepage hero needs to offer this visitor's installer directly. */
export interface HomeDownload {
  platform: DetectResult;
  version: string | null;
  assets: DownloadAssets;
  sizes: DownloadSizes;
}

export async function getHomeDownload(): Promise<HomeDownload> {
  const [release, platform] = await Promise.all([fetchLatestRelease(), getRequestPlatform()]);
  return { platform, version: release.version, assets: release.assets, sizes: release.sizes };
}
