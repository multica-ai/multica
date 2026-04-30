"use client";

import { Building2, Plus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { customerListOptions } from "@multica/core/customers/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useWorkspacePaths } from "@multica/core/paths";
import { useModalStore } from "@multica/core/modals";
import type { Customer } from "@multica/core/types";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import { PageHeader } from "../../layout/page-header";
import { AppLink } from "../../navigation";

function formatRelativeDate(date: string): string {
  const diff = Date.now() - new Date(date).getTime();
  const days = Math.floor(diff / (1000 * 60 * 60 * 24));
  if (days < 1) return "Today";
  if (days === 1) return "1d ago";
  if (days < 30) return `${days}d ago`;
  const months = Math.floor(days / 30);
  return `${months}mo ago`;
}

function primaryContact(customer: Customer): string {
  return customer.email || customer.phone || customer.website || "--";
}

function CustomerRow({ customer }: { customer: Customer }) {
  const wsPaths = useWorkspacePaths();

  return (
    <div className="group/row flex h-11 items-center gap-2 px-5 text-sm transition-colors hover:bg-accent/40">
      <AppLink href={wsPaths.customerDetail(customer.id)} className="flex min-w-0 flex-1 items-center gap-2">
        <span className="flex size-6 shrink-0 items-center justify-center rounded-md bg-muted text-muted-foreground">
          <Building2 className="h-3.5 w-3.5" />
        </span>
        <span className="min-w-0 flex-1 truncate font-medium">{customer.name}</span>
      </AppLink>
      <span className="w-24 shrink-0 text-center text-xs capitalize text-muted-foreground">{customer.status}</span>
      <span className="w-52 shrink-0 truncate text-xs text-muted-foreground">{primaryContact(customer)}</span>
      <span className="w-24 shrink-0 text-center text-xs text-muted-foreground tabular-nums">
        {customer.project_count}
      </span>
      <span className="w-20 shrink-0 text-right text-xs text-muted-foreground tabular-nums">
        {formatRelativeDate(customer.created_at)}
      </span>
    </div>
  );
}

export function CustomersPage() {
  const wsId = useWorkspaceId();
  const { data: customers = [], isLoading } = useQuery(customerListOptions(wsId));
  const openCreateCustomer = () => useModalStore.getState().open("create-customer");

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="justify-between px-5">
        <div className="flex items-center gap-2">
          <Building2 className="h-4 w-4 text-muted-foreground" />
          <h1 className="text-sm font-medium">Customers</h1>
          {!isLoading && customers.length > 0 && (
            <span className="text-xs text-muted-foreground tabular-nums">{customers.length}</span>
          )}
        </div>
        <Button size="sm" variant="outline" onClick={openCreateCustomer}>
          <Plus className="h-3.5 w-3.5 mr-1" />
          New customer
        </Button>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        {isLoading ? (
          <>
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5">
              <Skeleton className="h-3 w-12 flex-1 max-w-[48px]" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-24 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
              <Skeleton className="h-3 w-12 shrink-0" />
            </div>
            <div className="p-5 pt-1 space-y-1">
              {Array.from({ length: 4 }).map((_, i) => (
                <Skeleton key={i} className="h-11 w-full" />
              ))}
            </div>
          </>
        ) : customers.length === 0 ? (
          <div className="flex flex-col items-center justify-center py-24 text-muted-foreground">
            <Building2 className="h-10 w-10 mb-3 opacity-30" />
            <p className="text-sm">No customers yet</p>
            <Button size="sm" variant="outline" className="mt-3" onClick={openCreateCustomer}>
              Create your first customer
            </Button>
          </div>
        ) : (
          <>
            <div className="sticky top-0 z-[1] flex h-8 items-center gap-2 border-b bg-muted/30 px-5 text-xs font-medium text-muted-foreground">
              <span className="min-w-0 flex-1">Name</span>
              <span className="w-24 text-center shrink-0">Status</span>
              <span className="w-52 shrink-0">Contact</span>
              <span className="w-24 text-center shrink-0">Projects</span>
              <span className="w-20 text-right shrink-0">Created</span>
            </div>
            {customers.map((customer) => (
              <CustomerRow key={customer.id} customer={customer} />
            ))}
          </>
        )}
      </div>
    </div>
  );
}
