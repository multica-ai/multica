import type { IssueView } from "../api/schemas";

/**
 * The built-in "Unread" view: All issues filtered on the viewer's unread
 * marks. It is not stored on the server. The workspace issues page opens it
 * through the same `?view=` id as a saved view, and this definition stands
 * in for the saved one.
 */
export const UNREAD_VIEW_ID = "unread";

export const UNREAD_VIEW: IssueView = {
  id: UNREAD_VIEW_ID,
  workspace_id: "",
  owner_id: "",
  name: "",
  scope_type: "workspace",
  scope_id: null,
  scope_variant: null,
  visibility: "private",
  definition_version: 1,
  query: { unreadFilter: true },
  display: {},
  revision: 1,
  created_at: "",
  updated_at: "",
};

export function isUnreadView(view: Pick<IssueView, "id"> | null | undefined): boolean {
  return view?.id === UNREAD_VIEW_ID;
}
