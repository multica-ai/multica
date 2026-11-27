import type { Metadata } from "next";
import { SharedIssuePage } from "./shared-issue-page";

export const metadata: Metadata = {
  robots: { index: false, follow: false },
  referrer: "no-referrer",
};

export default SharedIssuePage;
