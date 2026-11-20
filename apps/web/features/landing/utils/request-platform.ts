import { headers } from "next/headers";
import { detectFromUserAgent, type DetectResult } from "./os-detect";

/** The visitor's platform from this request's headers, for server-rendered download buttons. */
export async function getRequestPlatform(): Promise<DetectResult> {
  const h = await headers();
  return detectFromUserAgent(h.get("user-agent") ?? "", {
    platform: h.get("sec-ch-ua-platform"),
    mobile: h.get("sec-ch-ua-mobile"),
  });
}
