export interface WorkspaceLink {
  id: string;
  workspaceId: string;
  projectId: string | null;
  name: string;
  icon: string;
  showName: boolean;
  urlTemplate: string;
  openMode: "embed" | "new_tab";
  allowSameOrigin: boolean;
  position: number;
}

export interface WorkspaceLinkInput {
  projectId?: string;
  name: string;
  icon: string;
  showName?: boolean;
  urlTemplate: string;
  openMode: "embed" | "new_tab";
  allowSameOrigin: boolean;
  position?: number;
}
