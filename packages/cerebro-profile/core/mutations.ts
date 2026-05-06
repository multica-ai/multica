// CEREBRO-PATCH(core-profile-mutations): cerebro modification of upstream file
import type { QueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import type { UserProfileRequest, UserProfileResponse } from "@multica/core/types";
import { profileKeys } from "./queries";

export function upsertMyProfileMutation(qc: QueryClient) {
  return {
    mutationFn: (data: UserProfileRequest) => api.upsertMyProfile(data),
    onSuccess: (saved: UserProfileResponse) => {
      qc.setQueryData(profileKeys.me(), saved);
    },
  };
}

export function deleteMyProfileMutation(qc: QueryClient) {
  return {
    mutationFn: () => api.deleteMyProfile(),
    onSuccess: () => {
      qc.setQueryData(profileKeys.me(), null);
    },
  };
}
