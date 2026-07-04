"use client";

import { useState } from "react";
import { useProjectDocuments, useCreateProjectDocument, useUpdateProjectDocument, useDeleteProjectDocument } from "@multica/core/documents";
import { Button } from "@multica/ui/components/ui/button";
import { ContentEditor } from "../../editor/content-editor";

export function ProjectDocsTab({ projectId }: { projectId: string }) {
  const { data: docs = [] } = useProjectDocuments(projectId);
  const createMut = useCreateProjectDocument(projectId);
  const updateMut = useUpdateProjectDocument(projectId);
  const deleteMut = useDeleteProjectDocument(projectId);
  
  const [selectedDocId, setSelectedDocId] = useState<string | null>(null);

  const selectedDoc = docs.find((d) => d.id === selectedDocId) || null;

  const handleCreate = () => {
    createMut.mutate({ title: "Untitled Document", content: "" }, {
      onSuccess: (newDoc) => setSelectedDocId(newDoc.id)
    });
  };

  return (
    <div className="flex h-full w-full overflow-hidden">
      {/* Sidebar for Document Tree */}
      <div className="w-64 border-r bg-muted/20 p-4 flex flex-col gap-4 overflow-y-auto">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-medium">Wiki</h3>
          <Button variant="ghost" size="icon-sm" onClick={handleCreate} disabled={createMut.isPending}>
            <span className="text-lg">+</span>
          </Button>
        </div>
        <div className="flex flex-col gap-1">
          {docs.length === 0 && (
            <p className="text-xs text-muted-foreground p-2">No documents yet.</p>
          )}
          {docs.map((d) => (
            <button
              key={d.id}
              onClick={() => setSelectedDocId(d.id)}
              className={`text-left px-2 py-1.5 rounded-md text-sm truncate ${selectedDocId === d.id ? 'bg-accent font-medium text-accent-foreground' : 'hover:bg-accent/50 text-muted-foreground'}`}
            >
              {d.title}
            </button>
          ))}
        </div>
      </div>
      
      {/* Editor Main Content */}
      <div className="flex-1 overflow-y-auto">
        {selectedDoc ? (
          <div className="max-w-4xl mx-auto py-12 px-8 flex flex-col gap-8 h-full">
            <div className="flex items-center justify-between">
              <input 
                type="text" 
                value={selectedDoc.title}
                onChange={(e) => updateMut.mutate({ documentId: selectedDoc.id, data: { title: e.target.value } })}
                className="text-4xl font-bold bg-transparent outline-none border-none placeholder:text-muted-foreground w-full"
                placeholder="Document Title"
              />
              <Button variant="ghost" className="text-destructive shrink-0" onClick={() => {
                deleteMut.mutate(selectedDoc.id);
                setSelectedDocId(null);
              }}>
                Delete
              </Button>
            </div>
            
            <div className="flex-1 min-h-[500px]">
              <ContentEditor
                key={selectedDoc.id}
                defaultValue={selectedDoc.content || ""}
                onUpdate={(newContent: string) => updateMut.mutate({ documentId: selectedDoc.id, data: { content: newContent } })}
                placeholder="Start writing..."
                className="min-h-[500px]"
                enableSlashCommands
              />
            </div>
          </div>
        ) : (
          <div className="flex h-full items-center justify-center text-muted-foreground">
            Select a document or create a new one
          </div>
        )}
      </div>
    </div>
  );
}
