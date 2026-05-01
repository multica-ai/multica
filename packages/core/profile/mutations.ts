import type { QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { UserProfileRequest, UserProfileResponse } from "../types";
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
