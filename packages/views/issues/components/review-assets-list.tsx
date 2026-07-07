import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Video, Image as ImageIcon, Check, Clock, AlertCircle, Trash2 } from "lucide-react";
import type { ReviewAsset } from "@multica/core/types";
import { listReviewAssetsOptions } from "@multica/core/reviews/queries";
import { useBulkApproveReviewAssets, useDeleteReviewAssetGroup, useReviewAssetUpload } from "@multica/core/reviews/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { UploadShowcase } from "../../reviews/upload-showcase";
import type { UploadPhase } from "../../reviews/upload-showcase";
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

interface ReviewAssetsListProps {
  workspaceId: string;
  issueId: string;
  onOpenAsset: (asset: ReviewAsset) => void;
}

export function ReviewAssetsList({ workspaceId, issueId, onOpenAsset }: ReviewAssetsListProps) {
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadPhase, setUploadPhase] = useState<UploadPhase | null>(null);
  const [uploadFile, setUploadFile] = useState<File | null>(null);
  const [pendingDelete, setPendingDelete] = useState<ReviewAsset | null>(null);

  const { data: assets, isLoading } = useQuery(listReviewAssetsOptions(workspaceId, issueId));
  const bulkApprove = useBulkApproveReviewAssets();
  const deleteGroup = useDeleteReviewAssetGroup();
  const uploadAsset = useReviewAssetUpload();

  // Auto-clear the showcase 2.5 s after a successful upload
  useEffect(() => {
    if (!uploadAsset.isSuccess) return;
    const t = setTimeout(() => {
      uploadAsset.reset();
      setUploadFile(null);
      setUploadPhase(null);
      setUploadProgress(0);
    }, 2500);
    return () => clearTimeout(t);
  }, [uploadAsset.isSuccess]); // eslint-disable-line react-hooks/exhaustive-deps

  if (isLoading) {
    return <div className="text-sm text-gray-500 animate-pulse">Loading review assets...</div>;
  }

  // Filter out older versions, only show the latest version per asset group
  const latestAssetsMap = new Map<string, ReviewAsset>();
  if (assets) {
    for (const a of assets) {
      const existing = latestAssetsMap.get(a.asset_group_id);
      if (!existing || existing.version < a.version) {
        latestAssetsMap.set(a.asset_group_id, a);
      }
    }
  }
  const latestAssets = Array.from(latestAssetsMap.values());

  const hasPending = latestAssets.some(a => a.status === "pending" || a.status === "changes_requested");


  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      setUploadFile(file);
      setUploadProgress(0);
      setUploadPhase(null);
      uploadAsset.mutate({
        workspaceId,
        issueId,
        file,
        onProgress: (p) => setUploadProgress(p),
        onPhaseChange: (phase) => setUploadPhase(phase),
      });
    }
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  const showUploadShowcase = uploadFile && (uploadAsset.isPending || uploadAsset.isSuccess || uploadAsset.isError);

  if (latestAssets.length === 0 && !uploadAsset.isSuccess) {
    return (
      <div className="mt-8">
        <input type="file" ref={fileInputRef} className="hidden" accept="video/*,image/*" onChange={handleFileChange} />

        {showUploadShowcase ? (
          <UploadShowcase
            file={uploadFile}
            phase={uploadPhase}
            progress={uploadProgress}
            isPending={uploadAsset.isPending}
            isSuccess={uploadAsset.isSuccess}
            isError={uploadAsset.isError}
          />
        ) : (
          <div className="flex flex-col items-center justify-center border-2 border-dashed border-border rounded-lg p-10 bg-muted/30">
            <Video className="w-10 h-10 text-muted-foreground mb-3" />
            <h3 className="text-sm font-semibold text-foreground">No media reviews yet</h3>
            <p className="text-xs text-muted-foreground mb-4 text-center max-w-sm">
              Upload a video or image to start a timestamped review and collaborate with your team.
            </p>
            <Button size="sm" onClick={() => fileInputRef.current?.click()}>
              <Plus className="w-4 h-4 mr-2" />
              Upload Asset
            </Button>
          </div>
        )}
      </div>
    );
  }

  return (
    <div className="mt-8 flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <h3 className="text-base font-semibold">Media Reviews</h3>
        <div className="flex gap-2">
          {hasPending && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => bulkApprove.mutate({ workspaceId, issueId })}
              disabled={bulkApprove.isPending}
            >
              <Check className="w-4 h-4 mr-2" />
              Approve All
            </Button>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => fileInputRef.current?.click()}
            disabled={uploadAsset.isPending}
          >
            <Plus className="w-4 h-4 mr-2" />
            Upload Asset
          </Button>
          <input type="file" ref={fileInputRef} className="hidden" accept="video/*,image/*" onChange={handleFileChange} />
        </div>
      </div>
      {/* Upload showcase above the grid while uploading */}
      {showUploadShowcase && (
        <UploadShowcase
          file={uploadFile}
          phase={uploadPhase}
          progress={uploadProgress}
          isPending={uploadAsset.isPending}
          isSuccess={uploadAsset.isSuccess}
          isError={uploadAsset.isError}
        />
      )}

      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
        {latestAssets.map((asset) => (
          <div
            key={asset.id}
            onClick={() => onOpenAsset(asset)}
            className="group relative flex flex-col gap-2 rounded-md border p-2 hover:border-gray-400 cursor-pointer transition-colors bg-white shadow-sm"
          >
            {/* Delete whole group — shown on hover */}
            <button
              className="absolute top-1.5 right-1.5 z-10 p-1 rounded bg-white/80 opacity-0 group-hover:opacity-100 hover:bg-red-50 hover:text-red-600 text-gray-400 transition-all"
              title="Delete media"
              onClick={(e) => {
                e.stopPropagation();
                setPendingDelete(asset);
              }}
            >
              <Trash2 className="w-3.5 h-3.5" />
            </button>
            <div className="relative aspect-video bg-gray-100 rounded flex items-center justify-center overflow-hidden">
              {asset.asset_type === "image" ? (
                <img src={asset.thumbnail_url || asset.src_url} alt={asset.name} className="object-cover w-full h-full" />
              ) : asset.thumbnail_url ? (
                <img src={asset.thumbnail_url} alt={asset.name} className="object-cover w-full h-full" />
              ) : asset.src_url ? (
                // ponytail: no server-side thumbnails yet; preload the first frame instead
                <video src={`${asset.src_url}#t=0.1`} preload="metadata" muted playsInline className="object-cover w-full h-full pointer-events-none" />
              ) : (
                <div className="text-gray-400">
                  {asset.asset_type === "video" ? <Video className="w-8 h-8" /> : <ImageIcon className="w-8 h-8" />}
                </div>
              )}
              {/* Hover overlay play button for video */}
              {asset.asset_type === "video" && (
                <div className="absolute inset-0 bg-black/0 group-hover:bg-black/20 flex items-center justify-center transition-colors">
                  <div className="w-8 h-8 rounded-full bg-black/50 text-white flex items-center justify-center opacity-0 group-hover:opacity-100 transition-opacity">
                    <Video className="w-4 h-4" />
                  </div>
                </div>
              )}
            </div>
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium truncate" title={asset.name}>
                  {asset.name}
                </span>
                <span className="text-xs bg-gray-100 px-1.5 py-0.5 rounded text-gray-600 font-medium">
                  v{asset.version}
                </span>
              </div>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-1">
                  {asset.status === "approved" && <Check className="w-3.5 h-3.5 text-green-500" />}
                  {asset.status === "changes_requested" && <AlertCircle className="w-3.5 h-3.5 text-red-500" />}
                  {asset.status === "pending" && <Clock className="w-3.5 h-3.5 text-yellow-500" />}
                  <span className="text-xs text-gray-500 capitalize">
                    {asset.status.replace("_", " ")}
                  </span>
                </div>
                {/* Could add comment count here if we aggregate it in the backend */}
              </div>
            </div>
          </div>
        ))}
      </div>

      <AlertDialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete media</AlertDialogTitle>
            <AlertDialogDescription>
              This will permanently delete <strong>{pendingDelete?.name}</strong> and all its versions. This action cannot be undone.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={() => {
                if (!pendingDelete) return;
                deleteGroup.mutate(
                  { workspaceId, issueId, assetGroupId: pendingDelete.asset_group_id },
                  { onSettled: () => setPendingDelete(null) }
                );
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
