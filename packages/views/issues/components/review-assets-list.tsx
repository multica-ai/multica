import { useQuery } from "@tanstack/react-query";
import { Plus, Video, Image as ImageIcon, Check, Clock, AlertCircle } from "lucide-react";
import { ReviewAsset } from "@multica/core/types";
import { listReviewAssetsOptions } from "@multica/core/reviews/queries";
import { useBulkApproveReviewAssets } from "@multica/core/reviews/mutations";
import { Button } from "@multica/ui/components/ui/button";

interface ReviewAssetsListProps {
  workspaceId: string;
  issueId: string;
  onOpenAsset: (asset: ReviewAsset) => void;
}

export function ReviewAssetsList({ workspaceId, issueId, onOpenAsset }: ReviewAssetsListProps) {
  const { data: assets, isLoading } = useQuery(listReviewAssetsOptions(workspaceId, issueId));
  const bulkApprove = useBulkApproveReviewAssets();

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

  if (latestAssets.length === 0) {
    return null; // Or show a placeholder if we want to encourage uploading
  }

  const hasPending = latestAssets.some(a => a.status === "pending" || a.status === "changes_requested");

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
          <Button variant="outline" size="sm">
            <Plus className="w-4 h-4 mr-2" />
            Upload Asset
          </Button>
        </div>
      </div>
      <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-4 gap-4">
        {latestAssets.map((asset) => (
          <div
            key={asset.id}
            onClick={() => onOpenAsset(asset)}
            className="group relative flex flex-col gap-2 rounded-md border p-2 hover:border-gray-400 cursor-pointer transition-colors bg-white shadow-sm"
          >
            <div className="relative aspect-video bg-gray-100 rounded flex items-center justify-center overflow-hidden">
              {asset.thumbnail_url ? (
                <img src={asset.thumbnail_url} alt={asset.name} className="object-cover w-full h-full" />
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
    </div>
  );
}
