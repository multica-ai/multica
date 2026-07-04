import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { useWorkspaceId } from "@multica/core/hooks";
import { listIssueTypesOptions } from "@multica/core/issue-types/queries";
import { useCreateIssueType, useUpdateIssueType, useDeleteIssueType } from "@multica/core/issue-types/mutations";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogFooter } from "@multica/ui/components/ui/dialog";
import { Plus, Trash2, Edit2 } from "lucide-react";
import { IssueTypeIcon } from "../../issues/components/issue-type-icon";
import type { IssueType } from "@multica/core/types";

export function IssueTypesTab() {
  const wsId = useWorkspaceId();
  const { data = [] } = useQuery(listIssueTypesOptions(wsId));
  const issueTypes = data as IssueType[];
  const [isCreateOpen, setIsCreateOpen] = useState(false);
  const [editingId, setEditingId] = useState<string | null>(null);

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("check-square");
  const [color, setColor] = useState("#6B7280");

  const createMutation = useCreateIssueType();
  const updateMutation = useUpdateIssueType();
  const deleteMutation = useDeleteIssueType();

  const handleOpenCreate = () => {
    setName("");
    setDescription("");
    setIcon("check-square");
    setColor("#6B7280");
    setEditingId(null);
    setIsCreateOpen(true);
  };

  const handleOpenEdit = (type: IssueType) => {
    setName(type.name);
    setDescription(type.description || "");
    setIcon(type.icon);
    setColor(type.color);
    setEditingId(type.id);
    setIsCreateOpen(true);
  };

  const handleSave = async () => {
    if (!name) return;
    
    if (editingId) {
      await updateMutation.mutateAsync({
        workspaceId: wsId,
        issueTypeId: editingId,
        ...{ name, description, icon, color },
      });
    } else {
      await createMutation.mutateAsync({
        workspaceId: wsId,
        name,
        description,
        icon,
        color,
        is_default: false,
        position: issueTypes.length,
      });
    }
    setIsCreateOpen(false);
  };

  const handleDelete = async (id: string) => {
    if (confirm("Are you sure you want to delete this issue type?")) {
      await deleteMutation.mutateAsync({ workspaceId: wsId, issueTypeId: id });
    }
  };

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-medium">Issue Types</h2>
          <p className="text-sm text-muted-foreground">
            Manage custom issue types for this workspace.
          </p>
        </div>
        <Button onClick={handleOpenCreate} size="sm">
          <Plus className="mr-2 h-4 w-4" />
          Create Type
        </Button>
      </div>

      <div className="border rounded-md divide-y">
        {issueTypes.map((type: IssueType) => (
          <div key={type.id} className="flex items-center justify-between p-4">
            <div className="flex items-center gap-3">
              <IssueTypeIcon icon={type.icon} color={type.color} className="h-5 w-5" />
              <div>
                <p className="font-medium text-sm">{type.name}</p>
                {type.description && (
                  <p className="text-xs text-muted-foreground">{type.description}</p>
                )}
              </div>
            </div>
            <div className="flex gap-2">
              <Button variant="ghost" size="sm" onClick={() => handleOpenEdit(type)}>
                <Edit2 className="h-4 w-4" />
              </Button>
              {!type.is_default && (
                <Button variant="ghost" size="sm" onClick={() => handleDelete(type.id)} className="text-red-500 hover:text-red-600">
                  <Trash2 className="h-4 w-4" />
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>

      <Dialog open={isCreateOpen} onOpenChange={setIsCreateOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingId ? "Edit Issue Type" : "Create Issue Type"}</DialogTitle>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <label className="text-sm font-medium">Name</label>
              <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="e.g. Campaign" />
            </div>
            <div className="space-y-2">
              <label className="text-sm font-medium">Description</label>
              <Input value={description} onChange={(e) => setDescription(e.target.value)} placeholder="Optional description" />
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <label className="text-sm font-medium">Icon ID</label>
                <Input value={icon} onChange={(e) => setIcon(e.target.value)} placeholder="e.g. sparkles" />
                <p className="text-xs text-muted-foreground">Supported: check-square, bug, sparkles, book-open, palette, file-text, megaphone</p>
              </div>
              <div className="space-y-2">
                <label className="text-sm font-medium">Color (Hex)</label>
                <Input value={color} onChange={(e) => setColor(e.target.value)} placeholder="#8B5CF6" />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setIsCreateOpen(false)}>Cancel</Button>
            <Button onClick={handleSave} disabled={!name}>Save</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
