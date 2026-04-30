export type CustomerStatus = "active" | "archived";

export interface Customer {
  id: string;
  workspace_id: string;
  name: string;
  description: string | null;
  website: string | null;
  email: string | null;
  phone: string | null;
  status: CustomerStatus;
  created_at: string;
  updated_at: string;
  project_count: number;
}

export interface CreateCustomerRequest {
  name: string;
  description?: string;
  website?: string;
  email?: string;
  phone?: string;
  status?: CustomerStatus;
}

export interface UpdateCustomerRequest {
  name?: string;
  description?: string | null;
  website?: string | null;
  email?: string | null;
  phone?: string | null;
  status?: CustomerStatus;
}

export interface ListCustomersResponse {
  customers: Customer[];
  total: number;
}
