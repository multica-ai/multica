import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { reviewKeys } from "./queries";

interface CreateReviewCommentParams {
  workspaceId: string;
  issueId: string;
  assetId: string;
  content: string;
  start_time?: number;
  end_time?: number;
  shapes?: any;
  parentId?: string;
}

export function useCreateReviewComment() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: CreateReviewCommentParams) => {
      return await api.createReviewComment(params.workspaceId, params.issueId, {
        asset_id: params.assetId,
        content: params.content,
        start_time: params.start_time,
        end_time: params.end_time,
        shapes: params.shapes,
        parent_id: params.parentId,
      });
    },
    onSuccess: (_newComment, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.comments(variables.workspaceId, variables.assetId),
      });
    },
  });
}

export function useResolveReviewComment() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; commentId: string; assetId: string }) => {
      return await api.resolveReviewComment(params.workspaceId, params.issueId, params.commentId);
    },
    onSuccess: (_resolvedComment, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.comments(variables.workspaceId, variables.assetId),
      });
    },
  });
}

export function useUnresolveReviewComment() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; commentId: string; assetId: string }) => {
      return await api.unresolveReviewComment(params.workspaceId, params.issueId, params.commentId);
    },
    onSuccess: (_unresolvedComment, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.comments(variables.workspaceId, variables.assetId),
      });
    },
  });
}

export function useUpdateReviewAssetStatus() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; assetId: string; status: string }) => {
      return await api.updateReviewAssetStatus(params.workspaceId, params.issueId, params.assetId, params.status);
    },
    onSuccess: (_updatedAsset, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.all(variables.workspaceId),
      });
    },
  });
}

export function useBulkApproveReviewAssets() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string }) => {
      return await api.bulkApproveReviewAssets(params.workspaceId, params.issueId);
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.all(variables.workspaceId),
      });
    },
  });
}

export function useDeleteReviewAsset() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; assetId: string }) => {
      return await api.deleteReviewAsset(params.workspaceId, params.issueId, params.assetId);
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: reviewKeys.all(variables.workspaceId) });
    },
  });
}

export function useDeleteReviewAssetGroup() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; assetGroupId: string }) => {
      return await api.deleteReviewAssetGroup(params.workspaceId, params.issueId, params.assetGroupId);
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({ queryKey: reviewKeys.all(variables.workspaceId) });
    },
  });
}

export function useReviewAssetUpload() {
  const queryClient = useQueryClient();

  return useMutation({
    mutationFn: async (params: { workspaceId: string; issueId: string; file: File; previousAssetId?: string; onProgress?: (progress: number) => void; onPhaseChange?: (phase: 'presigning' | 'uploading' | 'completing') => void }) => {
      const { workspaceId, issueId, file, previousAssetId, onProgress, onPhaseChange } = params;

      // 1. Presign
      onPhaseChange?.('presigning');
      const { upload_url, asset } = await api.presignReviewAssetUpload(workspaceId, issueId, {
        filename: file.name,
        content_type: file.type,
        size: file.size,
        previous_asset_id: previousAssetId,
      });

      // 2. Upload with XHR to track progress
      onPhaseChange?.('uploading');
      await new Promise<void>((resolve, reject) => {
        const xhr = new XMLHttpRequest();
        xhr.open("PUT", upload_url, true);
        xhr.setRequestHeader("Content-Type", file.type);

        xhr.upload.onprogress = (event) => {
          if (event.lengthComputable && onProgress) {
            const percentComplete = (event.loaded / event.total) * 100;
            onProgress(percentComplete);
          }
        };

        xhr.onload = () => {
          if (xhr.status >= 200 && xhr.status < 300) {
            resolve();
          } else {
            reject(new Error(`Failed to upload file to storage: ${xhr.status} ${xhr.statusText}`));
          }
        };

        xhr.onerror = () => reject(new Error("Network error during upload"));
        xhr.send(file);
      });

      // 3. Complete
      onPhaseChange?.('completing');
      await api.completeReviewAssetUpload(workspaceId, issueId, asset.id);
      return asset;
    },
    onSuccess: (_, variables) => {
      queryClient.invalidateQueries({
        queryKey: reviewKeys.all(variables.workspaceId),
      });
    },
  });
}
