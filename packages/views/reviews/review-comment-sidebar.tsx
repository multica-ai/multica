import React, { useState } from "react";
import { useModalStore } from "@multica/core/modals";
import { useAuthStore } from "@multica/core/auth";
import { 
  useCreateReviewComment, 
  useResolveReviewComment, 
  useUnresolveReviewComment,
  useDeleteReviewComment,
  useUpdateReviewComment
} from "@multica/core/reviews";
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
  const currentUserId = useAuthStore(s => s.user?.id);
  const { getActorName } = useActorName();
  const { mutate: createComment, isPending: isCreating } = useCreateReviewComment();
  const { mutate: resolveComment } = useResolveReviewComment();
  const { mutate: unresolveComment } = useUnresolveReviewComment();
  const { mutate: deleteComment } = useDeleteReviewComment();
  const { mutate: updateComment } = useUpdateReviewComment();
  
  const [draftContent, setDraftContent] = useState("");
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [filter, setFilter] = useState<"all" | "unresolved" | "resolved">("all");
  const [duration, setDuration] = useState(3);
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
  const [editContent, setEditContent] = useState("");

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

    createComment({
      workspaceId,
      issueId: asset.issue_id,
      assetId: asset.id,
      content: draftContent,
      start_time,
      end_time,
      shapes,
      parentId: replyingTo || undefined,
    });

    // Optimistically clear the form so the user can immediately type another comment
    setDraftContent("");
    setReplyingTo(null);
    clearCanvasShapes();
    editorRef.current?.clearContent();
  };

  return (
    <div className="flex flex-col w-full h-full bg-background text-foreground relative">
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
          <div className="text-muted-foreground text-sm">Loading comments...</div>
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
              <div className="text-muted-foreground text-sm text-center py-8">
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
                      ? 'ring-1 ring-primary' 
                      : comment.resolved 
                        ? 'bg-green-500/10 border-green-500/30' 
                        : 'bg-card border-border hover:border-primary/50'
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
                      <span className="font-medium text-sm text-foreground">
                        {getActorName("member", comment.author_id)}
                      </span>
                      <div className="flex items-center gap-2">
                        {comment.resolved && <span className="text-xs text-green-500 font-medium">Resolved</span>}
                        {comment.start_time !== undefined && comment.start_time !== null && (
                          <span 
                            className="text-xs text-primary bg-primary/10 px-1.5 rounded cursor-pointer hover:bg-primary/20"
                            onClick={(e) => {
                              e.stopPropagation();
                              onSeek(comment.start_time);
                            }}
                          >
                            {new Date(comment.start_time * 1000).toISOString().substring(14, 19)} - {new Date(comment.end_time * 1000).toISOString().substring(14, 19)}
                          </span>
                        )}
                      </div>
                    </div>
                    {editingCommentId === comment.id ? (
                      <div className="flex flex-col gap-2 mt-2">
                        <textarea 
                          value={editContent}
                          onChange={e => setEditContent(e.target.value)}
                          className="w-full bg-background border border-border rounded text-sm text-foreground p-2 min-h-[60px]"
                        />
                        <div className="flex gap-2 justify-end">
                          <button onClick={(e) => { e.stopPropagation(); setEditingCommentId(null); }} className="text-xs text-muted-foreground hover:text-foreground">Cancel</button>
                          <button 
                            onClick={(e) => {
                              e.stopPropagation();
                              updateComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id, content: editContent });
                              setEditingCommentId(null);
                            }} 
                            className="text-xs bg-primary text-primary-foreground px-2 py-1 rounded"
                          >
                            Save
                          </button>
                        </div>
                      </div>
                    ) : (
                      <p className="text-sm text-muted-foreground">{comment.content}</p>
                    )}
                  </div>
                  
                  {/* Actions */}
                  <div className="mt-2 flex gap-3 text-xs text-muted-foreground">
                    <button 
                      onClick={(e) => { 
                        e.stopPropagation(); 
                        setReplyingTo(comment.id); 
                        if (comment.start_time !== undefined && comment.start_time !== null) {
                          onSeek(comment.start_time);
                        }
                      }}
                      className="hover:text-primary"
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
                    {currentUserId === comment.author_id && (
                      <>
                        <button 
                          onClick={(e) => {
                            e.stopPropagation();
                            setEditContent(comment.content);
                            setEditingCommentId(comment.id);
                          }}
                          className="hover:text-primary"
                        >
                          Edit
                        </button>
                        <button 
                          onClick={(e) => { 
                            e.stopPropagation(); 
                            if (confirm("Are you sure you want to delete this comment?")) {
                              deleteComment({ workspaceId, issueId: asset.issue_id, commentId: comment.id, assetId: asset.id });
                            }
                          }}
                          className="hover:text-destructive"
                        >
                          Delete
                        </button>
                      </>
                    )}
                  </div>
                </div>

                {/* Replies */}
                {replies.length > 0 && (
                  <div className="pl-4 space-y-2 border-l-2 border-border ml-2">
                    {replies.map(reply => (
                      <div key={reply.id} className="bg-muted p-2 rounded border border-border text-sm">
                         <div className="font-medium text-xs text-foreground mb-1">
                           {getActorName("member", reply.author_id)}
                         </div>
                         {editingCommentId === reply.id ? (
                            <div className="flex flex-col gap-2 mt-1">
                              <textarea 
                                value={editContent}
                                onChange={e => setEditContent(e.target.value)}
                                className="w-full bg-background border border-border rounded text-xs text-foreground p-1.5 min-h-[40px]"
                              />
                              <div className="flex gap-2 justify-end">
                                <button onClick={(e) => { e.stopPropagation(); setEditingCommentId(null); }} className="text-[10px] text-muted-foreground hover:text-foreground">Cancel</button>
                                <button 
                                  onClick={(e) => {
                                    e.stopPropagation();
                                    updateComment({ workspaceId, issueId: asset.issue_id, commentId: reply.id, assetId: asset.id, content: editContent });
                                    setEditingCommentId(null);
                                  }} 
                                  className="text-[10px] bg-primary text-primary-foreground px-1.5 py-0.5 rounded"
                                >
                                  Save
                                </button>
                              </div>
                            </div>
                         ) : (
                           <p className="text-muted-foreground text-sm">{reply.content}</p>
                         )}
                         {currentUserId === reply.author_id && editingCommentId !== reply.id && (
                           <div className="mt-1 flex gap-2 text-[10px] text-muted-foreground">
                             <button onClick={(e) => { e.stopPropagation(); setEditContent(reply.content); setEditingCommentId(reply.id); }} className="hover:text-primary">Edit</button>
                             <button onClick={(e) => { e.stopPropagation(); if (confirm("Delete this reply?")) deleteComment({ workspaceId, issueId: asset.issue_id, commentId: reply.id, assetId: asset.id }); }} className="hover:text-destructive">Delete</button>
                           </div>
                         )}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            );
          });
        })()}
      </div>

      <div className="p-4 border-t border-border bg-background">
        {replyingTo && (
          <div className="flex justify-between items-center mb-2 bg-primary/10 p-2 rounded border border-primary/20">
            <span className="text-xs text-primary">Replying to thread</span>
            <button onClick={() => setReplyingTo(null)} className="text-xs text-primary hover:text-primary/80">Cancel</button>
          </div>
        )}
        <form onSubmit={handleSubmit} className="flex flex-col gap-2">
          <div 
            className="min-h-[80px] rounded border bg-card transition-colors cursor-text" 
            style={{ 
              borderColor: drawingShape?.color || 'hsl(var(--border))',
              boxShadow: drawingShape?.color ? `0 0 0 1px ${drawingShape.color}40` : undefined
            }}
            onFocus={onDrawStart}
            onClick={() => editorRef.current?.focus()}
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
              <div className="flex justify-between items-center bg-muted p-2 rounded border border-border">
                <span className="text-xs text-muted-foreground font-medium">
                  {new Date(currentTime * 1000).toISOString().substring(14, 19)}
                </span>
                <div className="flex items-center gap-2">
                  <label className="text-xs text-muted-foreground flex items-center gap-1.5 cursor-pointer hover:text-foreground transition-colors">
                    <input 
                      type="checkbox" 
                      checked={duration > 0} 
                      onChange={(e) => setDuration(e.target.checked ? 3 : 0)}
                      className="rounded bg-background border-border text-primary w-3 h-3 cursor-pointer"
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
                        className="w-12 text-xs border border-border bg-background rounded px-1.5 py-0.5 text-foreground focus:outline-none focus:border-primary"
                      />
                      <span className="text-xs text-muted-foreground">s</span>
                    </div>
                  )}
                </div>
              </div>
            )}
            <div className="flex justify-end">
              <button
                type="submit"
                disabled={isCreating || !draftContent.trim()}
                className="px-3 py-1.5 bg-primary text-primary-foreground text-sm font-medium rounded hover:bg-primary/90 disabled:opacity-50"
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
