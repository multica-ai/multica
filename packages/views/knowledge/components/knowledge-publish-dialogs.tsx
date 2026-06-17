"use client";

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Plus, Trash2 } from "lucide-react";
import { toast } from "sonner";
import { useWorkspaceId } from "@multica/core/hooks";
import { wikiPageListOptions } from "@multica/core/wiki/queries";
import { buildWikiTree, flattenWikiTree } from "@multica/core/wiki/tree";
import { skillListOptions } from "@multica/core/workspace/queries";
import {
  usePublishKnowledgeToSkill,
  usePublishKnowledgeToWiki,
} from "@multica/core/knowledge/mutations";
import type {
  KnowledgeDetail,
  KnowledgeSkillPublishFile,
  PublishKnowledgeToSkillRequest,
  PublishKnowledgeToWikiRequest,
} from "@multica/core/knowledge/types";
import { Button } from "@multica/ui/components/ui/button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@multica/ui/components/ui/select";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { useT } from "../../i18n";

const NONE_VALUE = "__none__";
const NEW_VALUE = "__new__";

function knowledgeMarkdown(detail: KnowledgeDetail | null): string {
  const item = detail?.item;
  if (!item) return "";
  return [
    `# ${item.title}`,
    item.problem_pattern && `## Problem pattern\n${item.problem_pattern}`,
    item.trigger_conditions && `## Trigger conditions\n${item.trigger_conditions}`,
    item.diagnostic_steps && `## Diagnostic steps\n${item.diagnostic_steps}`,
    item.recommended_practice && `## Recommended practice\n${item.recommended_practice}`,
    item.anti_patterns && `## Anti-patterns\n${item.anti_patterns}`,
    item.applicability && `## Applicability\n${item.applicability}`,
  ].filter(Boolean).join("\n\n");
}

function wikiDraft(detail: KnowledgeDetail | null): PublishKnowledgeToWikiRequest {
  return {
    title: detail?.item.title ?? "",
    content: knowledgeMarkdown(detail),
    wiki_page_id: null,
    parent_id: null,
  };
}

function skillDraft(detail: KnowledgeDetail | null): PublishKnowledgeToSkillRequest {
  return {
    name: detail?.item.title ?? "",
    description: detail?.item.problem_pattern ?? "",
    content: knowledgeMarkdown(detail),
    include_source_map: true,
    files: [],
    skill_id: null,
  };
}

export function KnowledgePublishWikiDialog({ detail }: { detail: KnowledgeDetail }) {
  const { t } = useT("knowledge");
  const wsId = useWorkspaceId();
  const pagesQuery = useQuery(wikiPageListOptions(wsId));
  const publishWiki = usePublishKnowledgeToWiki();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<PublishKnowledgeToWikiRequest>(() => wikiDraft(detail));

  useEffect(() => {
    if (open) setDraft(wikiDraft(detail));
  }, [detail.item.id, open]);

  const flatPages = useMemo(() => {
    const tree = buildWikiTree(pagesQuery.data ?? []);
    return flattenWikiTree(tree);
  }, [pagesQuery.data]);

  const submit = async () => {
    try {
      await publishWiki.mutateAsync({
        id: detail.item.id,
        ...draft,
        wiki_page_id: draft.wiki_page_id || null,
        parent_id: draft.parent_id || null,
      });
      toast.success(t(($) => $.toast.publish_wiki));
      setOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button size="sm" variant="outline" />}>
        {t(($) => $.detail.publish_wiki)}
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.publish_wiki.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.publish_wiki.description)}</DialogDescription>
        </DialogHeader>
        <div className="grid gap-4">
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_wiki.target)}</Label>
            <Select
              value={draft.wiki_page_id ?? NEW_VALUE}
              onValueChange={(value) =>
                setDraft((prev) => ({ ...prev, wiki_page_id: value === NEW_VALUE ? null : value }))
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NEW_VALUE}>{t(($) => $.publish_wiki.new_page)}</SelectItem>
                {flatPages.map((page) => (
                  <SelectItem key={page.id} value={page.id}>
                    {page.title}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_wiki.parent)}</Label>
            <Select
              value={draft.parent_id ?? NONE_VALUE}
              onValueChange={(value) =>
                setDraft((prev) => ({ ...prev, parent_id: value === NONE_VALUE ? null : value }))
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NONE_VALUE}>{t(($) => $.publish_wiki.no_parent)}</SelectItem>
                {flatPages.filter((page) => page.type === "folder").map((page) => (
                  <SelectItem key={page.id} value={page.id}>{page.title}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_wiki.page_title)}</Label>
            <Input
              value={draft.title ?? ""}
              onChange={(event) => setDraft((prev) => ({ ...prev, title: event.target.value }))}
            />
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_wiki.content)}</Label>
            <Textarea
              value={draft.content ?? ""}
              onChange={(event) => setDraft((prev) => ({ ...prev, content: event.target.value }))}
              className="min-h-72 font-mono text-sm"
            />
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>
            {t(($) => $.publish.cancel)}
          </Button>
          <Button onClick={submit} disabled={publishWiki.isPending || pagesQuery.isLoading}>
            {t(($) => $.publish.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

export function KnowledgePublishSkillDialog({ detail }: { detail: KnowledgeDetail }) {
  const { t } = useT("knowledge");
  const wsId = useWorkspaceId();
  const skillsQuery = useQuery(skillListOptions(wsId));
  const publishSkill = usePublishKnowledgeToSkill();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<PublishKnowledgeToSkillRequest>(() => skillDraft(detail));

  useEffect(() => {
    if (open) setDraft(skillDraft(detail));
  }, [detail.item.id, open]);

  const files = draft.files ?? [];
  const updateFile = (index: number, patch: Partial<KnowledgeSkillPublishFile>) => {
    setDraft((prev) => ({
      ...prev,
      files: (prev.files ?? []).map((file, i) => i === index ? { ...file, ...patch } : file),
    }));
  };
  const removeFile = (index: number) => {
    setDraft((prev) => ({ ...prev, files: (prev.files ?? []).filter((_, i) => i !== index) }));
  };

  const submit = async () => {
    try {
      await publishSkill.mutateAsync({
        id: detail.item.id,
        ...draft,
        skill_id: draft.skill_id || null,
        files: files.filter((file) => file.path.trim() !== ""),
      });
      toast.success(t(($) => $.toast.publish_skill));
      setOpen(false);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : t(($) => $.toast.action_failed));
    }
  };

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger render={<Button size="sm" variant="outline" />}>
        {t(($) => $.detail.publish_skill)}
      </DialogTrigger>
      <DialogContent className="sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle>{t(($) => $.publish_skill.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.publish_skill.description)}</DialogDescription>
        </DialogHeader>
        <div className="grid max-h-[70vh] gap-4 overflow-y-auto pr-1">
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_skill.target)}</Label>
            <Select
              value={draft.skill_id ?? NEW_VALUE}
              onValueChange={(value) =>
                setDraft((prev) => ({ ...prev, skill_id: value === NEW_VALUE ? null : value }))
              }
            >
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value={NEW_VALUE}>{t(($) => $.publish_skill.new_skill)}</SelectItem>
                {(skillsQuery.data ?? []).map((skill) => (
                  <SelectItem key={skill.id} value={skill.id}>{skill.name}</SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-3 md:grid-cols-2">
            <div className="grid gap-2">
              <Label>{t(($) => $.publish_skill.name)}</Label>
              <Input
                value={draft.name ?? ""}
                onChange={(event) => setDraft((prev) => ({ ...prev, name: event.target.value }))}
              />
            </div>
            <div className="flex items-end gap-2 pb-2">
              <Checkbox
                checked={draft.include_source_map !== false}
                onCheckedChange={(checked) =>
                  setDraft((prev) => ({ ...prev, include_source_map: checked === true }))
                }
              />
              <Label>{t(($) => $.publish_skill.include_source_map)}</Label>
            </div>
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_skill.description_label)}</Label>
            <Textarea
              value={draft.description ?? ""}
              onChange={(event) => setDraft((prev) => ({ ...prev, description: event.target.value }))}
              className="min-h-20"
            />
          </div>
          <div className="grid gap-2">
            <Label>{t(($) => $.publish_skill.content)}</Label>
            <Textarea
              value={draft.content ?? ""}
              onChange={(event) => setDraft((prev) => ({ ...prev, content: event.target.value }))}
              className="min-h-72 font-mono text-sm"
            />
          </div>
          <div className="grid gap-2">
            <div className="flex items-center justify-between gap-2">
              <Label>{t(($) => $.publish_skill.files)}</Label>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() =>
                  setDraft((prev) => ({
                    ...prev,
                    files: [...(prev.files ?? []), { path: "", content: "" }],
                  }))
                }
              >
                <Plus className="h-4 w-4" />
                {t(($) => $.publish_skill.add_file)}
              </Button>
            </div>
            {files.length === 0 ? (
              <p className="text-sm text-muted-foreground">{t(($) => $.publish_skill.no_files)}</p>
            ) : (
              <div className="grid gap-3">
                {files.map((file, index) => (
                  <div key={index} className="grid gap-2 rounded-lg border p-3">
                    <div className="flex gap-2">
                      <Input
                        value={file.path}
                        onChange={(event) => updateFile(index, { path: event.target.value })}
                        placeholder="references/example.md"
                        className="font-mono text-sm"
                      />
                      <Button type="button" size="icon-sm" variant="ghost" onClick={() => removeFile(index)}>
                        <Trash2 className="h-4 w-4" />
                      </Button>
                    </div>
                    <Textarea
                      value={file.content}
                      onChange={(event) => updateFile(index, { content: event.target.value })}
                      className="min-h-24 font-mono text-sm"
                    />
                  </div>
                ))}
              </div>
            )}
          </div>
        </div>
        <DialogFooter>
          <Button variant="outline" onClick={() => setOpen(false)}>
            {t(($) => $.publish.cancel)}
          </Button>
          <Button onClick={submit} disabled={publishSkill.isPending || skillsQuery.isLoading}>
            {t(($) => $.publish.submit)}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
