"use client";

import { useEffect, useState } from "react";
import { fetchGithubStars } from "@multica/core/github/stars";

export { formatStarCount } from "@multica/core/github/stars";

/**
 * Live GitHub star count for the landing header's "GitHub" button.
 *
 * The count is memoized at module scope (plus an in-flight promise) so
 * client-side navigation between landing pages reuses the first fetch. A failed fetch
 * caches `null` so we don't retry in a loop; the button just degrades to its
 * plain "GitHub" label.
 */

// `undefined` = never fetched; `number` = resolved count; `null` = fetch failed.
let cachedStars: number | null | undefined;
let inFlight: Promise<number | null> | null = null;

async function loadStars(): Promise<number | null> {
  if (cachedStars !== undefined) return cachedStars;
  if (inFlight) return inFlight;

  inFlight = fetchGithubStars()
    .then((count) => {
      cachedStars = count;
      return count;
    })
    .finally(() => {
      inFlight = null;
    });

  return inFlight;
}

export function useGithubStars(): number | null {
  const [stars, setStars] = useState<number | null>(cachedStars ?? null);

  useEffect(() => {
    let active = true;
    void loadStars().then((count) => {
      if (active && count != null) setStars(count);
    });
    return () => {
      active = false;
    };
  }, []);

  return stars;
}
