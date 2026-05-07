// CEREBRO-PATCH(core-profile-queries): cerebro modification of upstream file
import { queryOptions } from "@tanstack/react-query";
import { api } from "@multica/core/api";

export const profileKeys = {
  me: () => ["profile", "me"] as const,
};

// Returns null when the user has not saved a profile yet — the UI then
// falls back to buildDefaultProfile().
export function myProfileOptions() {
  return queryOptions({
    queryKey: profileKeys.me(),
    queryFn: () => api.getMyProfile(),
    staleTime: 60 * 1000,
  });
}
