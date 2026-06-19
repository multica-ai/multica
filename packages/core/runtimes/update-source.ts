export const CLI_UPDATE_REPO = "ethanturk/multica";

export const CLI_LATEST_RELEASE_URL = `https://api.github.com/repos/${CLI_UPDATE_REPO}/releases/latest`;

type FetchLike = typeof fetch;

export async function fetchLatestCliVersion(
  fetcher: FetchLike = fetch,
): Promise<string | null> {
  try {
    const resp = await fetcher(CLI_LATEST_RELEASE_URL, {
      headers: { Accept: "application/vnd.github+json" },
    });
    if (!resp.ok) return null;
    const data = (await resp.json()) as { tag_name?: unknown };
    return typeof data.tag_name === "string" ? data.tag_name : null;
  } catch {
    return null;
  }
}
