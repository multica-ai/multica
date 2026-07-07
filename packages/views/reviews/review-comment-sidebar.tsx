import React, { useState } from "react";
import { useModalStore } from "@multica/core/modals";
import { useCreateReviewComment, useResolveReviewComment, useUnresolveReviewComment } from "@multica/core/reviews";
import type { ReviewAsset } from "@multica/core/types";
import { useActorName } from "@multica/core/workspace";
import { ContentEditor, type ContentEditorRef } from "../editor";

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
  selectedCommentId?: string;
  onSelectComment?: (id: string) => void;
  drawingShape?: any;
}

export function ReviewCommentSidebar({
  workspaceId,
  asset,
  comments,
  isLoading,
  currentTime,
  onSeek,
  onDrawStart,
  getCanvasShapes,
  clearCanvasShapes,
  selectedCommentId,
  onSelectComment,
  drawingShape,
}: ReviewCommentSidebarProps) {
  const editorRef = React.useRef<ContentEditorRef>(null);
  const { getActorName } = useActorName();
  const { mutate: createComment, isPending: isCreating } = useCreateReviewComment();
  const { mutate: resolveComment } = useResolveReviewComment();
  const { mutate: unresolveComment } = useUnresolveReviewComment();
  const [draftContent, setDraftContent] = useState("");
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [filter, setFilter] = useState<"all" | "unresolved" | "resolved">("all");
  const [duration, setDuration] = useState(0);

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!draftContent.trim()) return;

    const shapes = getCanvasShapes() || [];
    let start_time = undefined;
    let end_time = undefined;
    if (asset.asset_type === "video") {
      start_time = currentTime;
      end_time = currentTime + duration;
    }

    createComment(
      {
        workspaceId,
        issueId: asset.issue_id,
        assetId: asset.id,
        content: draftContent,
        start_time,
        end_time,
        shapes,
        parentId: replyingTo || undefined,
      },
      {
        onSuccess: () => {
          setDraftContent("");
          setReplyingTo(null);
          clearCanvasShapes();
          editorRef.current?.clearContent();
        },
      }
    );
  };

  return (
    <div className="flex flex-col h-full bg-background text-foreground relative">
      {/* Header */}
      <div className="p-4 border-b border-border shrink-0 font-semibold flex justify-between items-center">
        <span>Review Comments</span>
        <select 
          className="text-xs border border-border bg-background rounded p-1"
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
              <div className="text-gray-400 text-sm text-center py-8">
                No comments match your filter.
              </div>
            );
          }

          return filteredParents.map((comment) => {
            const replies = comments?.filter(c => c.parent_id === comment.id) || [];
            const isSelected = selectedCommentId === comment.id;
            const shapeColor = comment.shapes?.[0]?.color;
            
            return (
              <div key={comment.id} className="space-y-2">
                {/* Parent Comment */}
                <div 
                  className={`p-3 rounded border shadow-sm transition-all cursor-pointer ${
                    isSelected 
                      ? 'ring-1' 
                      : comment.resolved 
                        ? 'bg-green-900/30 border-green-800' 
                        : 'bg-gray-800 border-gray-700 hover:border-blue-500'
                  }`}
                  style={isSelected ? {
                    borderColor: shapeColor || '#3b82f6',
                    boxShadow: `0 0 10px ${shapeColor || '#3b82f6'}40`,
                    backgroundColor: `${shapeColor || '#3b82f6'}20`
                  } : undefined}
                  onClick={() => onSelectComment?.(comment.id)}
                >
                  <div>
                    <div className="flex justify-between items-center mb-1">
                      <span className="font-medium text-sm text-gray-200">
                        {getActorName("member", comment.author_id)}
                      </span>
                      <div className="flex items-center gap-2">
                        {comment.resolved && <span className="text-xs text-green-500 font-medium">Resolved</span>}
                        {comment.start_time !== undefined && comment.start_time !== null && (
                          <span 
                            className="text-xs text-blue-300 bg-blue-900/50 px-1.5 rounded cursor-pointer hover:bg-blue-800"
                            onClick={(e) => {
                              e.stopPropagation();
                              onSeek(comment.start_time);
                            }}
                          >
                            {new Date(comment.start_time * 1000).toISOString().substr(14, 5)} - {new Date(comment.end_time * 1000).toISOString().substr(14, 5)}
                          </span>
                        )}
                      </div>
                    </div>
                    <p className="text-sm text-gray-300">{comment.content}</p>
                  </div>
                  
                  {/* Actions */}
                  <div className="mt-2 flex gap-3 text-xs text-gray-400">
                    <button 
                      onClick={(e) => { 
                        e.stopPropagation(); 
                        setReplyingTo(comment.id); 
                        if (comment.start_time !== undefined && comment.start_time !== null) {
                          onSeek(comment.start_time);
                        }
                      }}
                      className="hover:text-blue-400"
                    >
                      Reply
                    </button>
                    <button 
                      onClick={(e) => {
                        e.stopPropagation();
                        useModalStore.getState().open("create-issue", {
                          title: `Fix: ${comment.content.slice(0, 50)}...`,
                          description: `From review on ${asset.name}: \n\n> ${comment.content}`
                        });
                      }}
                      className="hover:text-purple-400"
                    >
                      Create Task
                    </button>
                    {comment.resolved ? (
                      <button 
                        onClick={(e) => { e.stopPropagation(); unresolveComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id }); }}
                        className="hover:text-red-400"
                      >
                        Unresolve
                      </button>
                    ) : (
                      <button 
                        onClick={(e) => { e.stopPropagation(); resolveComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id }); }}
                        className="hover:text-green-400"
                      >
                        Resolve
                      </button>
                    )}
                  </div>
                </div>

                {/* Replies */}
                {replies.length > 0 && (
                  <div className="pl-4 space-y-2 border-l-2 border-gray-700 ml-2">
                    {replies.map(reply => (
                      <div key={reply.id} className="bg-gray-800 p-2 rounded border border-gray-700 text-sm">
                         <div className="font-medium text-xs text-gray-200 mb-1">
                           {getActorName("member", reply.author_id)}
                         </div>
                         <p className="text-gray-300 text-sm">{reply.content}</p>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          });
        })()}
      </div>

      <div className="p-4 border-t border-gray-800 bg-gray-900">
        {replyingTo && (
          <div className="flex justify-between items-center mb-2 bg-blue-900/30 p-2 rounded border border-blue-800">
            <span className="text-xs text-blue-300">Replying to thread</span>
            <button onClick={() => setReplyingTo(null)} className="text-xs text-blue-400 hover:text-blue-200">Cancel</button>
          </div>
        )}
        <form onSubmit={handleSubmit} className="flex flex-col gap-2">
          <div 
            className="min-h-[80px] rounded border bg-gray-800 transition-colors" 
            style={{ 
              borderColor: drawingShape?.color || '#374151',
              boxShadow: drawingShape?.color ? `0 0 0 1px ${drawingShape.color}40` : undefined
            }}
            onFocus={onDrawStart}
          >
            <ContentEditor
              ref={editorRef}
              defaultValue={draftContent}
              placeholder="Add a review comment... (type @ to tag)"
              onUpdate={(md) => setDraftContent(md)}
              enableSlashCommands
              mentionMode="context"
              submitOnEnter
              onSubmit={() => {
                const fakeEvent = { preventDefault: () => {} } as React.FormEvent;
                handleSubmit(fakeEvent);
              }}
            />
          </div>
          <div className="flex flex-col gap-2">
            {asset.asset_type === "video" && (
              <div className="flex justify-between items-center bg-gray-800 p-2 rounded">
                <span className="text-xs text-gray-400 font-medium">
                  {new Date(currentTime * 1000).toISOString().substr(14, 5)}
                </span>
                <div className="flex items-center gap-2">
                  <label className="text-xs text-gray-400 flex items-center gap-1.5 cursor-pointer hover:text-gray-200 transition-colors">
                    <input 
                      type="checkbox" 
                      checked={duration > 0} 
                      onChange={(e) => setDuration(e.target.checked ? 3 : 0)}
                      className="rounded bg-gray-900 border-gray-700 text-blue-500 w-3 h-3 cursor-pointer"
                    />
                    Range
                  </label>
                  {duration > 0 && (
                    <div className="flex items-center gap-1">
                      <input
                        type="number"
                        min="1"
                        max="60"
                        value={duration}
                        onChange={(e) => setDuration(Number(e.target.value))}
                        className="w-12 text-xs border border-gray-700 bg-gray-900 rounded px-1.5 py-0.5 text-gray-200 focus:outline-none focus:border-blue-500"
                      />
                      <span className="text-xs text-gray-500">s</span>
                    </div>
                  )}
                </div>
              </div>
            )}
            <div className="flex justify-end">
              <button
                type="submit"
                disabled={isCreating || !draftContent.trim()}
                className="px-3 py-1.5 bg-blue-600 text-white text-sm font-medium rounded hover:bg-blue-700 disabled:opacity-50"
              >
                Comment
              </button>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
