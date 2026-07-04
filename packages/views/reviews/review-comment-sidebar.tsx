import React, { useState } from "react";
import { useCreateReviewComment, useResolveReviewComment, useUnresolveReviewComment } from "@multica/core/reviews";
import type { ReviewAsset } from "@multica/core/types";
import { ContentEditor } from "../editor";

interface ReviewCommentSidebarProps {
  workspaceId: string;
  asset: ReviewAsset;
  currentTime: number;
  onSeek: (time: number) => void;
  onDrawStart: () => void;
  getCanvasShapes: () => any;
  clearCanvasShapes: () => void;
  comments?: any[];
  isLoading?: boolean;
}

export function ReviewCommentSidebar({
  workspaceId,
  asset,
  comments,
  isLoading,
  currentTime,
  onSeek,
  onDrawStart,
  clearCanvasShapes,
}: ReviewCommentSidebarProps) {
  const { mutate: createComment, isPending: isCreating } = useCreateReviewComment();
  const { mutate: resolveComment } = useResolveReviewComment();
  const { mutate: unresolveComment } = useUnresolveReviewComment();
  const [draftContent, setDraftContent] = useState("");
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [filter, setFilter] = useState<"all" | "unresolved" | "resolved">("all");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!draftContent.trim()) return;

    createComment(
      {
        workspaceId,
        issueId: asset.issue_id,
        assetId: asset.id,
        content: draftContent,
        timestamp: asset.asset_type === "video" ? currentTime : undefined,
        parentId: replyingTo || undefined,
      },
      {
        onSuccess: () => {
          setDraftContent("");
          setReplyingTo(null);
          clearCanvasShapes();
        },
      }
    );
  };

  return (
    <div className="flex flex-col w-80 border-l border-gray-200 bg-white h-full overflow-hidden">
      <div className="p-4 border-b border-gray-200 font-semibold text-gray-900 flex justify-between items-center">
        <span>Review Comments</span>
        <select 
          className="text-xs border-gray-200 rounded text-gray-600 focus:ring-blue-500"
          value={filter}
          onChange={(e) => setFilter(e.target.value as any)}
        >
          <option value="all">All</option>
          <option value="unresolved">Unresolved</option>
          <option value="resolved">Resolved</option>
        </select>
      </div>
      
      <div className="flex-1 overflow-y-auto p-4 space-y-4">
        {isLoading ? (
          <div className="text-gray-500 text-sm">Loading comments...</div>
        ) : (() => {
          const parents = comments?.filter(c => !c.parent_id) || [];
          
          let filteredParents = parents;
          if (filter === "resolved") {
            filteredParents = parents.filter(p => p.resolved);
          } else if (filter === "unresolved") {
            filteredParents = parents.filter(p => !p.resolved);
          }

          if (filteredParents.length === 0) {
            return (
              <div className="text-gray-500 text-sm text-center py-8">
                No comments match your filter.
              </div>
            );
          }

          return filteredParents.map((comment) => {
            const replies = comments?.filter(c => c.parent_id === comment.id) || [];
            
            return (
              <div key={comment.id} className="space-y-2">
                {/* Parent Comment */}
                <div 
                  className={`p-3 rounded border shadow-sm transition-colors ${comment.resolved ? 'bg-green-50 border-green-100' : 'bg-gray-50 border-gray-100 hover:border-blue-300'}`}
                >
                  <div 
                    className="cursor-pointer"
                    onClick={() => {
                      if (comment.timestamp !== undefined && comment.timestamp !== null) {
                        onSeek(comment.timestamp);
                      }
                    }}
                  >
                    <div className="flex justify-between items-center mb-1">
                      <span className="font-medium text-sm text-gray-800">
                        User {comment.author_id.slice(0, 4)}
                      </span>
                      <div className="flex items-center gap-2">
                        {comment.resolved && <span className="text-xs text-green-600 font-medium">Resolved</span>}
                        {comment.timestamp !== undefined && comment.timestamp !== null && (
                          <span className="text-xs text-blue-600 bg-blue-50 px-1.5 rounded">
                            {new Date(comment.timestamp * 1000).toISOString().substr(14, 5)}
                          </span>
                        )}
                      </div>
                    </div>
                    <p className="text-sm text-gray-700">{comment.content}</p>
                  </div>
                  
                  {/* Actions */}
                  <div className="mt-2 flex gap-3 text-xs text-gray-500">
                    <button 
                      onClick={() => setReplyingTo(comment.id)}
                      className="hover:text-blue-600"
                    >
                      Reply
                    </button>
                    {comment.resolved ? (
                      <button 
                        onClick={() => unresolveComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id })}
                        className="hover:text-red-600"
                      >
                        Unresolve
                      </button>
                    ) : (
                      <button 
                        onClick={() => resolveComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id })}
                        className="hover:text-green-600"
                      >
                        Resolve
                      </button>
                    )}
                  </div>
                </div>

                {/* Replies */}
                {replies.length > 0 && (
                  <div className="pl-4 space-y-2 border-l-2 border-gray-100 ml-2">
                    {replies.map(reply => (
                      <div key={reply.id} className="bg-white p-2 rounded border border-gray-100 text-sm">
                         <div className="font-medium text-xs text-gray-800 mb-1">
                           User {reply.author_id.slice(0, 4)}
                         </div>
                         <p className="text-gray-700 text-sm">{reply.content}</p>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          });
        })()}
      </div>

      <div className="p-4 border-t border-gray-200 bg-gray-50">
        {replyingTo && (
          <div className="flex justify-between items-center mb-2 bg-blue-50 p-2 rounded border border-blue-100">
            <span className="text-xs text-blue-700">Replying to thread</span>
            <button onClick={() => setReplyingTo(null)} className="text-xs text-blue-500 hover:text-blue-700">Cancel</button>
          </div>
        )}
        <form onSubmit={handleSubmit} className="flex flex-col gap-2">
          <div className="min-h-[80px] rounded border border-gray-300 bg-white" onFocus={onDrawStart}>
            <ContentEditor
              defaultValue={draftContent}
              placeholder="Add a review comment..."
              onUpdate={(md) => setDraftContent(md)}
              enableSlashCommands
            />
          </div>
          <div className="flex justify-between items-center">
            {asset.asset_type === "video" && (
              <span className="text-xs text-gray-500">
                At {new Date(currentTime * 1000).toISOString().substr(14, 5)}
              </span>
            )}
            <button
              type="submit"
              disabled={isCreating || !draftContent.trim()}
              className="px-3 py-1.5 bg-blue-600 text-white text-sm font-medium rounded hover:bg-blue-700 disabled:opacity-50 ml-auto"
            >
              Comment
            </button>
          </div>
        </form>
      </div>
    </div>
  );
}
