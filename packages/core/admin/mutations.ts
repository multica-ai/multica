import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { adminKeys } from "./queries";

export function useUpdateUserName() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ userId, name }: { userId: string; name: string }) =>
      api.adminUpdateUser(userId, name),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: adminKeys.users() });
    },
  });
}
