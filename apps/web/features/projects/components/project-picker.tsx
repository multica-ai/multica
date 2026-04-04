"use client";

import { useState } from "react";
import { Check, FolderKanban, X } from "lucide-react";
import {
  Popover,
  PopoverTrigger,
  PopoverContent,
} from "@/components/ui/popover";
import {
  Command,
  CommandInput,
  CommandList,
  CommandEmpty,
  CommandGroup,
  CommandItem,
} from "@/components/ui/command";
import { useProjectStore } from "@/features/projects";

interface ProjectPickerProps {
  projectId: string | null;
  onUpdate: (updates: { project_id?: string | null }) => void;
  align?: "start" | "end";
}

export function ProjectPicker({ projectId, onUpdate, align = "start" }: ProjectPickerProps) {
  const projects = useProjectStore((s) => s.projects);
  const [open, setOpen] = useState(false);
  const current = projects.find((p) => p.id === projectId);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger className="flex items-center gap-1.5 cursor-pointer rounded px-1 -mx-1 hover:bg-accent/30 transition-colors overflow-hidden text-xs">
        {current ? (
          <>
            <div
              className="h-2.5 w-2.5 rounded-full shrink-0"
              style={{ backgroundColor: current.color ?? "#6366f1" }}
            />
            <span className="truncate">{current.name}</span>
          </>
        ) : (
          <span className="text-muted-foreground">None</span>
        )}
      </PopoverTrigger>
      <PopoverContent className="w-52 p-0" align={align}>
        <Command>
          <CommandInput placeholder="Search projects..." />
          <CommandList>
            <CommandEmpty>No projects found.</CommandEmpty>
            <CommandGroup>
              {projectId && (
                <CommandItem
                  onSelect={() => {
                    onUpdate({ project_id: null });
                    setOpen(false);
                  }}
                >
                  <X className="h-3.5 w-3.5 text-muted-foreground" />
                  <span className="text-muted-foreground">Remove from project</span>
                </CommandItem>
              )}
              {projects.map((p) => (
                <CommandItem
                  key={p.id}
                  onSelect={() => {
                    onUpdate({ project_id: p.id });
                    setOpen(false);
                  }}
                >
                  <div
                    className="h-2.5 w-2.5 rounded-full shrink-0"
                    style={{ backgroundColor: p.color ?? "#6366f1" }}
                  />
                  <span className="truncate">{p.name}</span>
                  {p.id === projectId && (
                    <Check className="ml-auto h-3.5 w-3.5 shrink-0" />
                  )}
                </CommandItem>
              ))}
            </CommandGroup>
          </CommandList>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
