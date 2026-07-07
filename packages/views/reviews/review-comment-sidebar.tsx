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
import { Clock, Pencil, Smile, Globe, ChevronDown, Send, X } from "lucide-react";

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
  const [endTime, setEndTime] = useState<number | null>(null);
  const [replyingTo, setReplyingTo] = useState<string | null>(null);
  const [filter, setFilter] = useState<"all" | "unresolved" | "resolved">("all");
  const [editingCommentId, setEditingCommentId] = useState<string | null>(null);
  const [editContent, setEditContent] = useState("");

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    if (!draftContent.trim()) return;

    let finalStartTime = (asset.asset_type === "video") && !replyingTo ? currentTime : null;
    let finalEndTime = (asset.asset_type === "video") && !replyingTo ? endTime : null;

    if (finalStartTime !== null && finalEndTime !== null && finalStartTime > finalEndTime) {
      const temp = finalStartTime;
      finalStartTime = finalEndTime;
      finalEndTime = temp;
    }

    createComment({
      workspaceId,
      issueId: asset.issue_id,
      assetId: asset.id,
      content: draftContent,
      start_time: finalStartTime !== null ? finalStartTime : undefined,
      end_time: finalEndTime !== null ? finalEndTime : undefined,
      shapes: getCanvasShapes() || [],
      parentId: replyingTo || undefined,
    });

    // Optimistically clear the form so the user can immediately type another comment
    setDraftContent("");
    setEndTime(null);
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
                    <div className="flex flex-col gap-1 mb-2">
                      <div className="flex justify-between items-center">
                        <span className="font-semibold text-[13px] text-foreground leading-none">
                          {getActorName("member", comment.author_id)}
                        </span>
                        {comment.resolved && <span className="text-[11px] text-green-500 font-medium leading-none">Resolved</span>}
                      </div>
                      <div className="flex items-center gap-1.5 flex-wrap">
                        {comment.start_time !== undefined && comment.start_time !== null && (
                          <button
                            className="inline-flex items-center gap-1 rounded-md bg-primary/15 px-1.5 py-0.5 text-[11px] font-mono text-primary hover:bg-primary/25 transition-colors"
                            onClick={(e) => {
                              e.stopPropagation();
                              onSeek(comment.start_time!);
                            }}
                            title="Jump to timecode"
                          >
                            <Clock className="w-2.5 h-2.5" />
                            {new Date(comment.start_time * 1000).toISOString().substring(14, 19)}
                            {comment.end_time !== null && comment.end_time !== undefined && comment.end_time !== comment.start_time && (
                              <> — {new Date(comment.end_time * 1000).toISOString().substring(14, 19)}</>
                            )}
                          </button>
                        )}
                        {comment.shapes && comment.shapes.length > 0 && (
                          <button
                            className="inline-flex items-center justify-center h-5 w-5 rounded text-purple-400/70 hover:text-purple-400 hover:bg-purple-500/15 transition-colors"
                            onClick={(e) => {
                              e.stopPropagation();
                              onSelectComment?.(comment.id);
                              if (comment.start_time !== undefined && comment.start_time !== null) {
                                onSeek(comment.start_time);
                              }
                            }}
                            title="Show annotation"
                          >
                            <Pencil className="w-3 h-3" />
                          </button>
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

      <div className="p-4 border-t border-border bg-background shrink-0">
        {replyingTo && (
          <div className="flex justify-between items-center mb-2 bg-accent/5 p-2 rounded border border-accent/10 text-xs text-accent">
            <span>Replying to thread</span>
            <button onClick={() => setReplyingTo(null)} className="text-muted-foreground hover:text-foreground transition-colors">
              <X className="w-3.5 h-3.5" />
            </button>
          </div>
        )}
        <form onSubmit={handleSubmit} className="flex flex-col gap-2">
          <div 
            className="flex flex-col rounded-xl border border-border bg-card shadow-sm focus-within:border-primary/40 focus-within:ring-4 focus-within:ring-primary/10 transition-all"
            style={{ 
              borderColor: drawingShape?.color || undefined,
              boxShadow: drawingShape?.color ? `0 0 0 1px ${drawingShape.color}40, 0 0 0 4px ${drawingShape.color}15` : undefined
            }}
          >
            <div 
              className="flex items-start gap-1 cursor-text min-h-[56px] pt-1"
              onFocus={onDrawStart}
              onClick={() => editorRef.current?.focus()}
            >
              {asset.asset_type === "video" && (
                <div className="shrink-0 ml-3 mt-[10px] mr-2 flex items-center gap-1">
                  <span className="rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[11px] font-medium text-amber-500 leading-none select-none border border-amber-500/20">
                    {new Date((endTime !== null ? Math.min(currentTime, endTime) : currentTime) * 1000).toISOString().substring(11, 19).replace(/^00:/, '')}
                  </span>
                  {endTime !== null && (
                    <>
                      <span className="text-muted-foreground text-[10px]">-</span>
                      <span className="rounded bg-amber-500/10 px-1.5 py-0.5 font-mono text-[11px] font-medium text-amber-500 leading-none select-none border border-amber-500/20">
                        {new Date(Math.max(currentTime, endTime) * 1000).toISOString().substring(11, 19).replace(/^00:/, '')}
                      </span>
                    </>
                  )}
                </div>
              )}
              <div className="flex-1 min-w-0 pt-0.5 pb-2">
                <ContentEditor
                  ref={editorRef}
                  defaultValue={draftContent}
                  placeholder={replyingTo ? "Write a reply..." : "Leave your comment..."}
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
            </div>
            
            {/* Bottom toolbar */}
            <div className="flex items-center justify-between px-2 pb-2 pt-1 border-t border-border/50">
              <div className="flex items-center gap-0.5">
                {asset.asset_type === "video" && (
                  <button
                    type="button"
                    onClick={() => setEndTime(endTime === null ? currentTime : null)}
                    className={`p-1.5 rounded-md transition-colors ${endTime !== null ? "text-amber-500 bg-amber-500/10" : "text-muted-foreground hover:text-foreground hover:bg-muted"}`}
                    title={endTime !== null ? "Remove end time" : "Set end time (duration)"}
                  >
                    <Clock className="w-4 h-4" />
                  </button>
                )}
                <button
                  type="button"
                  onClick={onDrawStart}
                  className={`p-1.5 rounded-md transition-colors ${drawingShape ? "text-purple-500 bg-purple-500/10" : "text-muted-foreground hover:text-foreground hover:bg-muted"}`}
                  title={drawingShape ? "Drawing active" : "Draw on frame"}
                >
                  <Pencil className="w-4 h-4" />
                </button>
                <button
                  type="button"
                  className="p-1.5 rounded-md text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
                  title="Add emoji"
                >
                  <Smile className="w-4 h-4" />
                </button>
              </div>
              
              <div className="flex items-center gap-2">
                <button
                  type="button"
                  className="flex items-center gap-1.5 px-2 py-1 text-[11px] font-medium text-muted-foreground rounded-md hover:bg-muted transition-colors"
                >
                  <Globe className="w-3.5 h-3.5" />
                  Public
                  <ChevronDown className="w-3 h-3 opacity-50" />
                </button>
                <button
                  type="submit"
                  disabled={isCreating || !draftContent.trim()}
                  className="w-7 h-7 flex items-center justify-center bg-primary text-primary-foreground rounded-full hover:bg-primary/90 disabled:opacity-50 transition-colors shadow-sm"
                  title="Send comment"
                >
                  <Send className="w-3 h-3 -ml-0.5" />
                </button>
              </div>
            </div>
          </div>
        </form>
      </div>
    </div>
  );
}
