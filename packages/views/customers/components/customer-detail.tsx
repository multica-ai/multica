"use client";

import { Building2, ChevronRight, Trash2 } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import { customerDetailOptions } from "@multica/core/customers/queries";
import { useDeleteCustomer, useUpdateCustomer } from "@multica/core/customers/mutations";
import { projectListOptions } from "@multica/core/projects/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import { useCurrentWorkspace, useWorkspacePaths } from "@multica/core/paths";
import { Button } from "@multica/ui/components/ui/button";
import { Skeleton } from "@multica/ui/components/ui/skeleton";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@multica/ui/components/ui/alert-dialog";
import { useState } from "react";
import { PageHeader } from "../../layout/page-header";
import { AppLink, useNavigation } from "../../navigation";
import { TitleEditor, ContentEditor } from "../../editor";
import { ProjectIcon } from "../../projects/components/project-icon";

function FieldInput({
  value,
  placeholder,
  onCommit,
}: {
  value: string | null;
  placeholder: string;
  onCommit: (value: string | null) => void;
}) {
  const [draft, setDraft] = useState(value ?? "");
  return (
    <input
      value={draft}
      placeholder={placeholder}
      onChange={(event) => setDraft(event.target.value)}
      onBlur={() => {
        const next = draft.trim() || null;
        if (next !== value) onCommit(next);
      }}
      className="w-full bg-transparent text-sm placeholder:text-muted-foreground outline-none"
    />
  );
}

export function CustomerDetail({ customerId }: { customerId: string }) {
  const wsId = useWorkspaceId();
  const wsPaths = useWorkspacePaths();
  const workspace = useCurrentWorkspace();
  const nav = useNavigation();
  const { data: customer, isLoading } = useQuery(customerDetailOptions(wsId, customerId));
  const { data: projects = [] } = useQuery(projectListOptions(wsId));
  const updateCustomer = useUpdateCustomer();
  const deleteCustomer = useDeleteCustomer();
  const [deleteOpen, setDeleteOpen] = useState(false);

  if (isLoading) {
    return (
      <div className="flex h-full flex-col">
        <PageHeader className="px-5"><Skeleton className="h-4 w-40" /></PageHeader>
        <div className="p-6 space-y-4">
          <Skeleton className="h-8 w-64" />
          <Skeleton className="h-24 w-full" />
        </div>
      </div>
    );
  }

  if (!customer) {
    return <div className="flex h-full items-center justify-center text-muted-foreground">Customer not found</div>;
  }

  const linkedProjects = projects.filter((project) => project.customer_id === customer.id);
  const update = (data: Parameters<typeof updateCustomer.mutate>[0] extends { id: string } & infer R ? R : never) => {
    updateCustomer.mutate({ id: customer.id, ...data });
  };

  return (
    <div className="flex h-full flex-col">
      <PageHeader className="gap-2 bg-background text-sm">
        <div className="flex flex-1 items-center gap-1.5 min-w-0">
          <AppLink href={wsPaths.customers()} className="text-muted-foreground hover:text-foreground transition-colors shrink-0">
            {workspace?.name ?? "Customers"}
          </AppLink>
          <ChevronRight className="h-3 w-3 text-muted-foreground/50 shrink-0" />
          <span className="truncate">{customer.name}</span>
        </div>
        <Button variant="ghost" size="icon-sm" className="text-muted-foreground" onClick={() => setDeleteOpen(true)}>
          <Trash2 />
        </Button>
      </PageHeader>

      <div className="flex-1 overflow-y-auto">
        <div className="mx-auto w-full max-w-3xl px-6 py-6">
          <div className="mb-5 flex items-center gap-3">
            <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted text-muted-foreground">
              <Building2 className="h-5 w-5" />
            </span>
            <TitleEditor
              key={customer.id}
              defaultValue={customer.name}
              placeholder="Customer name"
              className="text-xl font-semibold"
              onBlur={(value) => {
                const name = value.trim();
                if (name && name !== customer.name) update({ name });
              }}
            />
          </div>

          <div className="mb-6 rounded-md border">
            <div className="grid grid-cols-[110px_1fr] items-center border-b px-3 py-2">
              <span className="text-xs text-muted-foreground">Status</span>
              <select
                value={customer.status}
                onChange={(event) => update({ status: event.target.value as "active" | "archived" })}
                className="w-fit bg-transparent text-sm outline-none"
              >
                <option value="active">Active</option>
                <option value="archived">Archived</option>
              </select>
            </div>
            <div className="grid grid-cols-[110px_1fr] items-center border-b px-3 py-2">
              <span className="text-xs text-muted-foreground">Website</span>
              <FieldInput value={customer.website} placeholder="Website" onCommit={(website) => update({ website })} />
            </div>
            <div className="grid grid-cols-[110px_1fr] items-center border-b px-3 py-2">
              <span className="text-xs text-muted-foreground">Email</span>
              <FieldInput value={customer.email} placeholder="Email" onCommit={(email) => update({ email })} />
            </div>
            <div className="grid grid-cols-[110px_1fr] items-center px-3 py-2">
              <span className="text-xs text-muted-foreground">Phone</span>
              <FieldInput value={customer.phone} placeholder="Phone" onCommit={(phone) => update({ phone })} />
            </div>
          </div>

          <div className="mb-6">
            <div className="mb-2 text-xs font-medium text-muted-foreground">Notes</div>
            <div className="rounded-md border px-3 py-2">
              <ContentEditor
                key={`${customer.id}-description`}
                defaultValue={customer.description ?? ""}
                placeholder="Add notes..."
                onUpdate={(md) => update({ description: md || null })}
                debounceMs={1000}
              />
            </div>
          </div>

          <div>
            <div className="mb-2 flex items-center gap-2 text-xs font-medium text-muted-foreground">
              <span>Projects</span>
              <span className="tabular-nums">{linkedProjects.length}</span>
            </div>
            <div className="rounded-md border">
              {linkedProjects.length === 0 ? (
                <div className="px-3 py-8 text-center text-sm text-muted-foreground">No linked projects</div>
              ) : (
                linkedProjects.map((project) => (
                  <AppLink
                    key={project.id}
                    href={wsPaths.projectDetail(project.id)}
                    className="flex items-center gap-2 border-b px-3 py-2 text-sm last:border-b-0 hover:bg-accent/40"
                  >
                    <ProjectIcon project={project} size="md" />
                    <span className="min-w-0 flex-1 truncate">{project.title}</span>
                    <span className="text-xs capitalize text-muted-foreground">{project.status.replaceAll("_", " ")}</span>
                  </AppLink>
                ))
              )}
            </div>
          </div>
        </div>
      </div>

      <AlertDialog open={deleteOpen} onOpenChange={setDeleteOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>Delete customer?</AlertDialogTitle>
            <AlertDialogDescription>
              This removes the customer and unlinks it from projects. The projects themselves will remain.
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Cancel</AlertDialogCancel>
            <AlertDialogAction
              onClick={() => {
                deleteCustomer.mutate(customer.id, {
                  onSuccess: () => {
                    toast.success("Customer deleted");
                    nav.push(wsPaths.customers());
                  },
                });
              }}
            >
              Delete
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  );
}
