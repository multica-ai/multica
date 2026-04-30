"use client";

import { Building2, Check, UserMinus } from "lucide-react";
import { useQuery } from "@tanstack/react-query";
import { customerListOptions } from "@multica/core/customers/queries";
import { useWorkspaceId } from "@multica/core/hooks";
import type { UpdateProjectRequest } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";

export function CustomerPicker({
  customerId,
  onUpdate,
  className,
}: {
  customerId: string | null;
  onUpdate: (data: UpdateProjectRequest) => void;
  className?: string;
}) {
  const wsId = useWorkspaceId();
  const { data: customers = [] } = useQuery(customerListOptions(wsId));
  const current = customers.find((c) => c.id === customerId);

  return (
    <DropdownMenu>
      <DropdownMenuTrigger
        render={
          <button
            type="button"
            className={cn("inline-flex items-center gap-1.5 text-xs hover:text-foreground transition-colors", className)}
          >
            <Building2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className={current ? "" : "text-muted-foreground"}>
              {current?.name ?? "No customer"}
            </span>
          </button>
        }
      />
      <DropdownMenuContent align="start" className="w-56">
        {customers.map((customer) => (
          <DropdownMenuItem key={customer.id} onClick={() => onUpdate({ customer_id: customer.id })}>
            <Building2 className="h-3.5 w-3.5 text-muted-foreground" />
            <span className="min-w-0 flex-1 truncate">{customer.name}</span>
            {customer.id === customerId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
          </DropdownMenuItem>
        ))}
        {customers.length > 0 && customerId && <DropdownMenuSeparator />}
        {customerId && (
          <DropdownMenuItem onClick={() => onUpdate({ customer_id: null })}>
            <UserMinus className="h-3.5 w-3.5 text-muted-foreground" />
            <span>No customer</span>
          </DropdownMenuItem>
        )}
        {customers.length === 0 && (
          <div className="px-2 py-1.5 text-xs text-muted-foreground">No customers yet</div>
        )}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
