export type AppScope = { resource_type: string; resource_id: string; access: "read" | "write" | "read_write" };
export type AppView = { id: string; type: "form" | "lookup" | "approval"; title: string; schema?: Record<string, unknown> };
export type AppManifest = { schema_version: "1"; name: string; version: string; scopes: AppScope[]; views?: AppView[]; frontend?: { entry: string }; backend?: { entry: string } };

export type CatalogApp = {
  id: string;
  slug: string;
  name: string;
  description: string;
  icon: string;
  folder: string;
  current_version?: string;
  status: "draft" | "published" | "disabled";
};

export type AppAdminSummary = {
  id: string; name: string; owner: string; version: string; status: string;
  approved_scopes: AppScope[]; spend_cents: number; runs: number; failed_runs: number;
  health: "healthy" | "attention" | "disabled"; touched: string[];
};
export type AppFolder = { id: string; parent_id?: string | null; name: string };

export type AppVersion = {
  version: string;
  release_notes: string;
  grant_status: "pending" | "approved" | "revoked" | "not_requested";
  scopes: AppScope[];
  created_at?: string;
};

export type AppDetail = CatalogApp & { versions: AppVersion[] };
