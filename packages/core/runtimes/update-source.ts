import { api } from "../api";

export const LATEST_CLI_VERSION_ENDPOINT = "/api/runtimes/latest-version";

export async function fetchLatestCliVersion(): Promise<string | null> {
  try {
    const data = await api.getLatestCliRelease();
    return typeof data.tag_name === "string" ? data.tag_name : null;
  } catch {
    return null;
  }
}
