"use client";

import { useId, useState } from "react";
import { useCreateCodeGraphRepository } from "@multica/core/workspace/mutations";
import type { GraphAuthInput } from "@multica/core/workspace/codegraph";
import { Plus } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Field, FieldGroup, FieldLabel } from "@multica/ui/components/ui/field";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { GitAuthFields, GitNotice } from "./git-credentials";
import { useT } from "../../i18n";

export function RegisterRepositoryDialog({ wsId }: { wsId: string }) {
  const { t } = useT("layout");
  const fieldId = useId();
  const [open, setOpen] = useState(false);
  const [repoUrl, setRepoUrl] = useState("");
  const [branch, setBranch] = useState("main");
  const [auth, setAuth] = useState<GraphAuthInput>({
    auth_mode: "token",
    repo_url: "",
    branch: "main",
  });
  const createRepo = useCreateCodeGraphRepository(wsId);
  const canSubmit =
    repoUrl.trim() !== "" &&
    branch.trim() !== "" &&
    (auth.auth_mode === "anonymous" || !!auth.credential_id);
  async function submit() {
    if (!canSubmit || createRepo.isPending) return;
    try {
      await createRepo.mutateAsync({
        repo_url: repoUrl.trim(),
        branch: branch.trim(),
        auth_mode: auth.auth_mode,
        credential_id: auth.credential_id,
      });
      setRepoUrl("");
      setBranch("main");
      setAuth({ auth_mode: "token", repo_url: "", branch: "main" });
      setOpen(false);
    } catch {
      // Preserve the draft while the mutation exposes its error below.
    }
  }
  return (
    <Dialog
      open={open}
      onOpenChange={(value) => {
        if (!createRepo.isPending) setOpen(value);
      }}
    >
      <DialogTrigger render={<Button />}>
        <Plus className="size-4" />
        {t(($) => $.code_graph.register)}
      </DialogTrigger>
      <DialogContent
        className="flex max-h-[85dvh] min-h-0 flex-col gap-0 overflow-hidden p-0 sm:max-w-lg"
        aria-describedby={undefined}
      >
        <form
          className="flex min-h-0 flex-col overflow-hidden"
          onSubmit={(event) => {
            event.preventDefault();
            void submit();
          }}
        >
          <DialogHeader className="shrink-0 border-b px-6 py-5">
            <DialogTitle>{t(($) => $.code_graph.register_title)}</DialogTitle>
          </DialogHeader>
          <FieldGroup className="min-h-0 overflow-y-auto px-6 py-5">
            <Field>
              <FieldLabel htmlFor={fieldId + "-url"}>
                {t(($) => $.code_graph.git_url)}
              </FieldLabel>
              <Input
                id={fieldId + "-url"}
                required
                value={repoUrl}
                onChange={(event) => setRepoUrl(event.target.value)}
                placeholder="https://gitlab.example.com/namespace/repo.git"
              />
            </Field>
            <Field>
              <FieldLabel htmlFor={fieldId + "-branch"}>
                {t(($) => $.code_graph.branch)}
              </FieldLabel>
              <Input
                id={fieldId + "-branch"}
                required
                value={branch}
                onChange={(event) => setBranch(event.target.value)}
              />
            </Field>
            <GitAuthFields
              value={{
                ...auth,
                repo_url: repoUrl.trim(),
                branch: branch.trim(),
              }}
              onChange={setAuth}
            />
            <GitNotice>{t(($) => $.code_graph.git_auth.import_hint)}</GitNotice>
            {createRepo.error && (
              <p role="alert" className="text-body text-destructive">
                {createRepo.error.message}
              </p>
            )}
          </FieldGroup>
          <DialogFooter className="mx-0 mb-0 shrink-0 px-6 py-4">
            <Button
              type="button"
              variant="outline"
              disabled={createRepo.isPending}
              onClick={() => setOpen(false)}
            >
              {t(($) => $.code_graph.cancel)}
            </Button>
            <Button
              type="submit"
              disabled={!canSubmit || createRepo.isPending}
              aria-busy={createRepo.isPending}
            >
              {t(($) => $.code_graph.register)}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
