import { useQuery } from "@tanstack/react-query";
import { getTestAsUserAccess } from "../api";

// useTestAsUserAccess reports whether the current member may use the Test as
// user feature, so the profile menu can show or hide the entry. `enabled` lets
// the caller fold in the feature flag without skipping the hook. Defaults to
// hidden until the access check confirms an explicit Allow.
export function useTestAsUserAccess(wsId: string, enabled: boolean): boolean {
  const { data } = useQuery({
    queryKey: ["cerebro", "test-as-user-access", wsId],
    queryFn: () => getTestAsUserAccess(wsId),
    enabled: enabled && !!wsId,
  });
  return data === true;
}
