"use client";

// CEREBRO-PATCH(settings-mobile-nav): mobile settings tab picker and global sidebar trigger.

import React from "react";
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectLabel,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { MobileSidebarTrigger } from "../../layout/page-header";

export interface CerebroMobileTabNavItem {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
}

export interface CerebroMobileTabNavGroup {
  label: string;
  items: CerebroMobileTabNavItem[];
}

interface CerebroMobileTabNavProps {
  value: string;
  onValueChange: (next: string) => void;
  groups: CerebroMobileTabNavGroup[];
}

export function CerebroMobileTabNav({
  value,
  onValueChange,
  groups,
}: CerebroMobileTabNavProps) {
  return (
    <div className="sticky top-0 z-20 flex items-center gap-2 border-b bg-background p-3 md:hidden">
      <MobileSidebarTrigger className="mr-0 shrink-0" />
      <Select
        value={value}
        onValueChange={(next) => {
          if (next) onValueChange(next);
        }}
      >
        <SelectTrigger className="min-w-0 flex-1">
          <SelectValue />
        </SelectTrigger>
        <SelectContent align="start" className="max-h-[min(28rem,var(--available-height))]">
          {groups.map((group) => (
            <SelectGroup key={group.label}>
              <SelectLabel>{group.label}</SelectLabel>
              {group.items.map((item) => {
                const Icon = item.icon;
                return (
                  <SelectItem key={item.value} value={item.value}>
                    <Icon className="h-4 w-4" />
                    {item.label}
                  </SelectItem>
                );
              })}
            </SelectGroup>
          ))}
        </SelectContent>
      </Select>
    </div>
  );
}
