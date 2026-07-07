import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Trash2 } from "lucide-react";
import type { ReviewAsset } from "@multica/core/types";
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
import { useUpdateReviewAssetStatus, useReviewAssetUpload, useDeleteReviewAsset, useDeleteReviewAssetGroup } from "@multica/core/reviews/mutations";
import { listReviewAssetsOptions, listReviewCommentsOptions } from "@multica/core/reviews/queries";
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@multica/ui/components/ui/select";
import { MediaReviewPlayer, type MediaReviewPlayerRef } from "./media-review-player";
import { ReviewCommentSidebar } from "./review-comment-sidebar";
import { UploadShowcase } from "./upload-showcase";
import { ResizablePanelGroup, ResizablePanel, ResizableHandle } from "@multica/ui/components/ui/resizable";
import type { UploadPhase } from "./upload-showcase";

interface MediaReviewLayoutProps {
  workspaceId: string;
  asset: ReviewAsset;
  onAssetChange?: (asset: ReviewAsset) => void;
  onClose?: () => void;
}

export function MediaReviewLayout({ workspaceId, asset, onAssetChange, onClose }: MediaReviewLayoutProps) {
  const playerRef = useRef<MediaReviewPlayerRef>(null);
  const [currentTime, setCurrentTime] = useState(0);
  const [selectedCommentId, setSelectedCommentId] = useState<string | undefined>();
  const [drawingShape, setDrawingShape] = useState<any>(null);

  const { data: allAssets } = useQuery(listReviewAssetsOptions(workspaceId, asset.issue_id));
  const { data: comments, isLoading: commentsLoading } = useQuery(listReviewCommentsOptions(workspaceId, asset.issue_id, asset.id));
  const updateStatus = useUpdateReviewAssetStatus();
  const uploadAsset = useReviewAssetUpload();
  const deleteAsset = useDeleteReviewAsset();
  const deleteGroup = useDeleteReviewAssetGroup();
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [uploadProgress, setUploadProgress] = useState(0);
  const [uploadPhase, setUploadPhase] = useState<UploadPhase | null>(null);
  const [uploadFile, setUploadFile] = useState<File | null>(null);

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
  const [pendingDelete, setPendingDelete] = useState<'version' | 'group' | null>(null);

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (file) {
      setUploadFile(file);
      setUploadProgress(0);
      setUploadPhase(null);
      uploadAsset.mutate(
        {
          workspaceId,
          issueId: asset.issue_id,
          file,
          previousAssetId: assetVersions[0]?.id ?? asset.id,
          onProgress: (p) => setUploadProgress(p),
          onPhaseChange: (phase) => setUploadPhase(phase),
        },
        {
          onSuccess: (newAsset) => {
            if (onAssetChange) onAssetChange(newAsset);
          },
        }
      );
    }
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  const assetVersions = (allAssets as ReviewAsset[] | undefined)
    ?.filter((a: ReviewAsset) => a.asset_group_id === asset.asset_group_id)
    ?.sort((a: ReviewAsset, b: ReviewAsset) => b.version - a.version) || [asset];

  const handleSeek = (time: number) => {
    playerRef.current?.seek(time);
  };

  const handleDrawStart = () => {
    playerRef.current?.pause();
  };

  const getCanvasShapes = () => {
    return playerRef.current?.getCanvasShapes();
  };

  const clearCanvasShapes = () => {
    playerRef.current?.clearCanvasShapes();
  };

  const handleStatusChange = (status: any) => {
    updateStatus.mutate({
      workspaceId,
      issueId: asset.issue_id,
      assetId: asset.id,
      status
    });
  };

  const handleVersionChange = (assetId: any) => {
    const selected = assetVersions.find((a: ReviewAsset) => a.id === assetId);
    if (selected && onAssetChange) {
      onAssetChange(selected);
    }
  };

  const handleDeleteVersion = () => setPendingDelete('version');
  const handleDeleteGroup = () => setPendingDelete('group');

  const confirmDelete = () => {
    if (pendingDelete === 'version') {
      deleteAsset.mutate(
        { workspaceId, issueId: asset.issue_id, assetId: asset.id },
        {
          onSuccess: () => {
            setPendingDelete(null);
            const remaining = assetVersions.filter((v) => v.id !== asset.id);
            if (remaining.length > 0 && remaining[0] && onAssetChange) {
              onAssetChange(remaining[0]);
            } else {
              onClose?.();
            }
          },
        }
      );
    } else if (pendingDelete === 'group') {
      deleteGroup.mutate(
        { workspaceId, issueId: asset.issue_id, assetGroupId: asset.asset_group_id },
        { onSuccess: () => { setPendingDelete(null); onClose?.(); } }
      );
    }
  };

  return (
    <div className="flex flex-col h-full w-full bg-background">
      {/* Review Asset Header */}
      <div className="h-14 border-b border-border bg-muted/20 flex items-center justify-between px-4 text-foreground">
        <div className="flex items-center gap-4">
          <div className="font-medium text-sm">{asset.name}</div>
          
          <div className="flex items-center gap-2">
            <Select value={asset.id} onValueChange={handleVersionChange}>
              <SelectTrigger className="h-7 border-gray-700 bg-gray-800 text-xs w-28">
                <SelectValue placeholder="Version" />
              </SelectTrigger>
              <SelectContent>
                {assetVersions.map((v) => (
                  <SelectItem key={v.id} value={v.id}>
                    Version {v.version}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>

            <button
              onClick={() => fileInputRef.current?.click()}
              disabled={uploadAsset.isPending}
              className="text-xs px-2 py-1 bg-gray-800 hover:bg-gray-700 rounded border border-gray-700 text-gray-300"
            >
              {uploadAsset.isPending ? "Uploading…" : "Upload New Version"}
            </button>

            {/* Delete current version */}
            <button
              onClick={handleDeleteVersion}
              disabled={deleteAsset.isPending}
              title="Delete this version"
              className="text-xs px-2 py-1 bg-gray-800 hover:bg-red-900 hover:text-red-400 rounded border border-gray-700 text-gray-400 flex items-center gap-1"
            >
              <Trash2 className="w-3 h-3" />
              Version
            </button>
            <input 
              type="file" 
              ref={fileInputRef} 
              className="hidden" 
              accept="video/*,image/*" 
              onChange={handleFileChange} 
            />
          </div>
        </div>

        <div className="flex items-center gap-2">
          {/* Delete entire media group */}
          <button
            onClick={handleDeleteGroup}
            disabled={deleteGroup.isPending}
            title="Delete all versions of this media"
            className="p-1.5 rounded hover:bg-red-900/50 hover:text-red-400 text-gray-500 transition-colors"
          >
            <Trash2 className="w-4 h-4" />
          </button>

          <Select value={asset.status} onValueChange={handleStatusChange}>
            <SelectTrigger className={`h-8 text-xs font-medium border-0 focus:ring-0 ${
              asset.status === 'approved' ? 'bg-green-500/20 text-green-400' :
              asset.status === 'changes_requested' ? 'bg-red-500/20 text-red-400' :
              'bg-yellow-500/20 text-yellow-400'
            }`}>
              <SelectValue placeholder="Status" />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="pending">Pending</SelectItem>
              <SelectItem value="changes_requested">Changes Requested</SelectItem>
              <SelectItem value="approved">Approved</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      {/* Review Content */}
      <ResizablePanelGroup direction="horizontal" className="flex-1 w-full overflow-hidden">
        <ResizablePanel defaultSize={75} minSize={50} className="relative bg-black flex flex-col overflow-auto">
          {/* Upload showcase overlaid above player while uploading a new version */}
          {uploadFile && (uploadAsset.isPending || uploadAsset.isSuccess || uploadAsset.isError) && (
            <div className="absolute inset-x-0 top-0 z-20 p-4">
              <UploadShowcase
                file={uploadFile}
                phase={uploadPhase}
                progress={uploadProgress}
                isPending={uploadAsset.isPending}
                isSuccess={uploadAsset.isSuccess}
                isError={uploadAsset.isError}
              />
            </div>
          )}
          <MediaReviewPlayer
            ref={playerRef}
            asset={asset}
            onTimeUpdate={setCurrentTime}
            comments={comments as any[]}
            selectedCommentId={selectedCommentId}
            onSelectComment={setSelectedCommentId}
            onDrawingShapeChange={setDrawingShape}
          />
        </ResizablePanel>
        
        <ResizableHandle withHandle className="bg-border" />
        
        <ResizablePanel defaultSize={25} minSize={20} maxSize={40} className="bg-background">
          <ReviewCommentSidebar
            workspaceId={workspaceId}
            asset={asset}
            comments={comments as any[]}
            isLoading={commentsLoading}
            currentTime={currentTime}
            selectedCommentId={selectedCommentId}
            onSelectComment={setSelectedCommentId}
            onSeek={handleSeek}
            onDrawStart={handleDrawStart}
            getCanvasShapes={getCanvasShapes}
            clearCanvasShapes={clearCanvasShapes}
            drawingShape={drawingShape}
          />
        </ResizablePanel>
      </ResizablePanelGroup>

      <AlertDialog open={!!pendingDelete} onOpenChange={(open) => !open && setPendingDelete(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {pendingDelete === 'version' ? 'Delete version' : 'Delete media'}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {pendingDelete === 'version'
                ? <>This will permanently delete <strong>version {asset.version}</strong> of &ldquo;{asset.name}&rdquo; and all its annotations.</>
                : <>This will permanently delete <strong>{asset.name}</strong> and all its versions. This action cannot be undone.</>
              }
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              className="bg-destructive text-white hover:bg-destructive/90"
              onClick={confirmDelete}
              disabled={deleteAsset.isPending || deleteGroup.isPending}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
