import { useState } from "react";
import { Check } from "lucide-react";
import { useGitHubRepos } from "@multica/core/github/queries";
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from "@multica/ui/components/ui/command";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { GitHubMark } from "../../../settings/components/github-mark";

export function GitHubRepoPicker({
  selectedRepoFullName,
  onSelect,
  triggerRender,
}: {
  selectedRepoFullName?: string;
  onSelect: (repo: any) => void;
  triggerRender: React.ReactElement;
}) {
  const [open, setOpen] = useState(false);
  const { data: repos } = useGitHubRepos();

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger render={triggerRender} />
      <PopoverContent className="w-[280px] p-0" align="start">
        <Command>
          <CommandInput placeholder="Search repositories..." />
          <CommandList>
            <CommandEmpty>No repositories found.</CommandEmpty>
            <CommandGroup>
              {repos?.map((repo) => (
                <CommandItem
                  key={repo.id}
                  value={repo.full_name}
                  onSelect={() => {
                    onSelect(repo);
                    setOpen(false);
                  }}
                  className="flex items-center justify-between"
                >
                  <div className="flex items-center gap-2 truncate">
                    <GitHubMark className="size-4 shrink-0 text-muted-foreground" />
                    <span className="truncate">{repo.full_name}</span>
                  </div>
                  {selectedRepoFullName === repo.full_name && (
                    <Check className="size-4 shrink-0" />
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
