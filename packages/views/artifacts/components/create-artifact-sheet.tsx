"use client";

import * as React from "react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { Label } from "@multica/ui/components/ui/label";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
  SheetDescription,
  SheetFooter,
} from "@multica/ui/components/ui/sheet";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { useCreateArtifact } from "@multica/core/artifacts/mutations";
import type { ArtifactKind } from "@multica/core/types";
import { KIND_LABELS } from "./kind-icon";
import { KIND_TEMPLATES, KIND_HELP } from "./kind-templates";

const KINDS: ArtifactKind[] = ["report", "plan", "decision", "diagram", "note"];

type Scope =
  | { issueId: string; projectId?: undefined; workspaceScoped?: false }
  | { issueId?: undefined; projectId: string; workspaceScoped?: false }
  | { issueId?: undefined; projectId?: undefined; workspaceScoped: true };

export type CreateArtifactSheetProps = Scope & {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultKind?: ArtifactKind;
};

export function CreateArtifactSheet(props: CreateArtifactSheetProps) {
  const create = useCreateArtifact();

  const [kind, setKind] = React.useState<ArtifactKind>(
    props.defaultKind ?? "report",
  );
  const [title, setTitle] = React.useState("");
  const [body, setBody] = React.useState(
    KIND_TEMPLATES[props.defaultKind ?? "report"],
  );
  // Track whether the user has hand-edited the body. We only auto-fill the
  // template when switching kind on a fresh body — overwriting user content
  // would be hostile.
  const [bodyDirty, setBodyDirty] = React.useState(false);

  React.useEffect(() => {
    if (!props.open) {
      const k = props.defaultKind ?? "report";
      setKind(k);
      setTitle("");
      setBody(KIND_TEMPLATES[k]);
      setBodyDirty(false);
    }
  }, [props.open, props.defaultKind]);

  const handleKindChange = (next: ArtifactKind) => {
    setKind(next);
    if (!bodyDirty) setBody(KIND_TEMPLATES[next]);
  };

  const handleBodyChange = (value: string) => {
    setBody(value);
    setBodyDirty(value !== KIND_TEMPLATES[kind]);
  };

  const scopeLabel = props.issueId
    ? "this issue"
    : props.projectId
      ? "this project"
      : "the workspace";

  const canSubmit = title.trim().length > 0 && !create.isPending;

  const handleCreate = async () => {
    await create.mutateAsync({
      kind,
      title: title.trim(),
      body,
      ...(props.issueId ? { issue_id: props.issueId } : {}),
      ...(props.projectId ? { project_id: props.projectId } : {}),
      // workspaceScoped: omit both ids → server stores as workspace scope.
    });
    props.onOpenChange(false);
  };

  return (
    <Sheet open={props.open} onOpenChange={props.onOpenChange}>
      <SheetContent className="w-full sm:max-w-2xl">
        <SheetHeader>
          <SheetTitle>New artifact</SheetTitle>
          <SheetDescription>
            Create a typed document scoped to {scopeLabel}.
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-1 flex-col gap-3 overflow-y-auto px-4">
          <div className="flex flex-col gap-1">
            <Label htmlFor="artifact-kind">Kind</Label>
            <Select
              value={kind}
              onValueChange={(v) => handleKindChange(v as ArtifactKind)}
            >
              <SelectTrigger id="artifact-kind">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k} value={k}>
                    {KIND_LABELS[k]}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <p className="text-xs text-muted-foreground">{KIND_HELP[kind]}</p>
          </div>

          <div className="flex flex-col gap-1">
            <Label htmlFor="artifact-title">Title</Label>
            <Input
              id="artifact-title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              placeholder="Short, descriptive title"
              autoFocus
            />
          </div>

          <div className="flex flex-1 flex-col gap-1">
            <Label htmlFor="artifact-body">Body (markdown)</Label>
            <Textarea
              id="artifact-body"
              value={body}
              onChange={(e) => handleBodyChange(e.target.value)}
              placeholder="Markdown content"
              className="min-h-[300px] flex-1 font-mono text-sm"
            />
          </div>
        </div>

        <SheetFooter className="flex-row justify-end border-t">
          <Button
            variant="ghost"
            size="sm"
            onClick={() => props.onOpenChange(false)}
            disabled={create.isPending}
          >
            Cancel
          </Button>
          <Button size="sm" onClick={handleCreate} disabled={!canSubmit}>
            {create.isPending ? "Creating…" : "Create"}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
