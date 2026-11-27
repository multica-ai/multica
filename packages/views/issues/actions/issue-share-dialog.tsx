"use client";

import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, ApiError } from "@multica/core/api";
import { useAuthStore } from "@multica/core/auth";
import { issueKeys } from "@multica/core/issues/queries";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { toast } from "sonner";
import { useT } from "../../i18n";

export function IssueShareDialog({
  data,
  open,
  onClose,
  onOpenChangeComplete,
}: {
  data: Record<string, unknown> | null;
  open: boolean;
  onClose: () => void;
  onOpenChangeComplete: (open: boolean) => void;
}) {
  const { t } = useT("issues");
  const { t: common } = useT("common");
  const userId = useAuthStore((state) => state.user?.id);
  const queryClient = useQueryClient();
  const wsId = typeof data?.wsId === "string" ? data.wsId : "";
  const issueId = typeof data?.issueId === "string" ? data.issueId : "";
  const token = typeof data?.token === "string" ? data.token : null;
  const key = issueKeys.share(wsId, issueId, userId ?? null);
  const mutation = useMutation({
    mutationFn: async () => {
      if (!token) return api.createIssueShare(wsId, issueId);
      await api.revokeIssueShare(wsId, issueId, token);
      return { token: null };
    },
  });

  if (!userId || userId !== data?.userId || !wsId || !issueId) return null;

  const confirm = async () => {
    try {
      const share = await mutation.mutateAsync();
      await queryClient.cancelQueries({ queryKey: key, exact: true });
      queryClient.setQueryData(key, share);
      toast.success(token ? t(($) => $.sharing.revoked) : t(($) => $.sharing.generated));
      onClose();
    } catch (error) {
      if (error instanceof ApiError && [403, 404, 409].includes(error.status)) {
        void queryClient.invalidateQueries({ queryKey: key, exact: true });
        toast.error(t(($) => $.sharing.changed));
        onClose();
      } else {
        toast.error(t(($) => $.sharing.save_failed));
      }
    }
  };

  return (
    <AlertDialog
      open={open}
      onOpenChange={(nextOpen) => { if (!nextOpen && !mutation.isPending) onClose(); }}
      onOpenChangeComplete={onOpenChangeComplete}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{token ? t(($) => $.sharing.revoke) : t(($) => $.sharing.generate)}</AlertDialogTitle>
          <AlertDialogDescription>
            {token ? t(($) => $.sharing.revoke_description) : t(($) => $.sharing.generate_description)}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel disabled={mutation.isPending}>{common(($) => $.cancel)}</AlertDialogCancel>
          <AlertDialogAction disabled={mutation.isPending} aria-busy={mutation.isPending} onClick={confirm}>
            {token ? t(($) => $.sharing.revoke_confirm) : t(($) => $.sharing.generate_confirm)}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
