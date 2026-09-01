"use client";

import { useState } from "react";
import { Bot, Check, ChevronLeft, ChevronRight, Loader2, ShieldCheck, Users } from "lucide-react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";
import { api } from "@multica/core/api";
import { marketplaceTemplateKeys } from "@multica/core/templates";
import { agentListOptions, squadListOptions } from "@multica/core/workspace/queries";
import type {
  Agent,
  MarketplaceTemplateSourceType,
  MarketplaceTemplateVisibility,
  Squad,
} from "@multica/core/types";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { cn } from "@multica/ui/lib/utils";
import { AvatarUploadControl } from "../../common/avatar-upload-control";
import { useT } from "../../i18n";

const MIN_TEMPLATE_DESCRIPTION = 50;

type WizardStep = 1 | 2 | 3 | 4;

function TypeCard({
  icon: Icon,
  title,
  description,
  selected,
  onClick,
}: {
  icon: typeof Bot;
  title: string;
  description: string;
  selected: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={cn(
        "flex min-h-52 flex-col items-start rounded-xl border p-6 text-left transition-colors hover:bg-muted/40",
        selected ? "border-foreground ring-1 ring-foreground" : "border-border",
      )}
    >
      <span className="flex size-12 items-center justify-center rounded-xl bg-muted">
        <Icon className="size-6" aria-hidden="true" />
      </span>
      <span className="mt-7 text-title font-medium">{title}</span>
      <span className="mt-2 text-body leading-relaxed text-muted-foreground">{description}</span>
    </button>
  );
}

function Stepper({ step }: { step: WizardStep }) {
  const { t } = useT("templates");
  const items = [
    t(($) => $.create.steps.type),
    t(($) => $.create.steps.source),
    t(($) => $.create.steps.info),
    t(($) => $.create.steps.confirm),
  ];
  return (
    <div className="grid grid-cols-4 border-y bg-muted/20 px-8 py-4">
      {items.map((label, index) => {
        const number = (index + 1) as WizardStep;
        const active = number === step;
        const complete = number < step;
        return (
          <div key={label} className="flex min-w-0 items-center gap-3">
            <span
              className={cn(
                "flex size-8 shrink-0 items-center justify-center rounded-full border text-caption font-medium",
                active && "border-foreground text-foreground ring-1 ring-foreground",
                complete && "border-foreground bg-foreground text-background",
                !active && !complete && "text-muted-foreground",
              )}
            >
              {complete ? <Check className="size-4" /> : number}
            </span>
            <span className={cn("truncate text-body", active ? "font-medium text-foreground" : "text-muted-foreground")}>{label}</span>
          </div>
        );
      })}
    </div>
  );
}

export function CreateTemplateDialog({
  open,
  onOpenChange,
  wsId,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  wsId: string;
}) {
  const { t } = useT("templates");
  const queryClient = useQueryClient();
  const { data: agents = [] } = useQuery(agentListOptions(wsId));
  const { data: squads = [] } = useQuery(squadListOptions(wsId));
  const [step, setStep] = useState<WizardStep>(1);
  const [sourceType, setSourceType] = useState<MarketplaceTemplateSourceType>("agent");
  const [sourceId, setSourceId] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const [visibility, setVisibility] = useState<MarketplaceTemplateVisibility>("private");
  const [imageURL, setImageURL] = useState<string | null>(null);

  const sources: Array<Agent | Squad> = sourceType === "agent"
    ? agents.filter((agent) => !agent.archived_at)
    : squads;
  const source = sources.find((item) => item.id === sourceId) ?? null;

  const reset = () => {
    setStep(1);
    setSourceType("agent");
    setSourceId("");
    setName("");
    setDescription("");
    setTags("");
    setVisibility("private");
    setImageURL(null);
  };
  const setOpen = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const mutation = useMutation({
    mutationFn: () => api.createMarketplaceTemplate({
      source_type: sourceType,
      source_id: sourceId,
      name: name.trim(),
      description: description.trim(),
      tags: tags.split(/[,，]/).map((tag) => tag.trim()).filter(Boolean).slice(0, 8),
      visibility,
      image_url: imageURL,
    }),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: marketplaceTemplateKeys.all(wsId) });
      toast.success(t(($) => $.create.success));
      setOpen(false);
    },
  });

  const chooseType = (next: MarketplaceTemplateSourceType) => {
    if (next !== sourceType) {
      setSourceType(next);
      setSourceId("");
      setName("");
      setDescription("");
      setImageURL(null);
    }
  };
  const chooseSource = (item: Agent | Squad) => {
    setSourceId(item.id);
    setName(item.name);
    setDescription(item.description ?? "");
  };

  const descriptionLength = Array.from(description.trim()).length;
  const canContinue = step === 1
    || (step === 2 && source !== null)
    || (step === 3 && name.trim() !== "" && descriptionLength >= MIN_TEMPLATE_DESCRIPTION)
    || step === 4;
  const sourceAgentCount = sourceType === "agent" ? 1 : (source as Squad | null)?.member_count ?? 0;
  const sourceSkillCount = sourceType === "agent" ? (source as Agent | null)?.skills.length ?? 0 : null;

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent className="max-h-[90vh] gap-0 overflow-hidden p-0 sm:max-w-4xl">
        <DialogHeader className="px-8 py-6 text-left">
          <DialogTitle className="text-title">{t(($) => $.create.title)}</DialogTitle>
          <DialogDescription>{t(($) => $.create.description)}</DialogDescription>
        </DialogHeader>
        <Stepper step={step} />

        <div className="min-h-[420px] overflow-y-auto px-8 py-7">
          {step === 1 ? (
            <div>
              <h3 className="text-title font-medium">{t(($) => $.create.select_type_title)}</h3>
              <p className="mt-1 text-body text-muted-foreground">{t(($) => $.create.select_type_description)}</p>
              <div className="mt-6 grid gap-4 md:grid-cols-2">
                <TypeCard
                  icon={Bot}
                  title={t(($) => $.page.agent_type)}
                  description={t(($) => $.create.agent_type_description)}
                  selected={sourceType === "agent"}
                  onClick={() => chooseType("agent")}
                />
                <TypeCard
                  icon={Users}
                  title={t(($) => $.page.squad_type)}
                  description={t(($) => $.create.squad_type_description)}
                  selected={sourceType === "squad"}
                  onClick={() => chooseType("squad")}
                />
              </div>
            </div>
          ) : null}

          {step === 2 ? (
            <div>
              <h3 className="text-title font-medium">{t(($) => $.create.select_source_title)}</h3>
              <p className="mt-1 text-body text-muted-foreground">{t(($) => $.create.select_source_description)}</p>
              {sources.length === 0 ? (
                <div className="mt-8 rounded-xl border border-dashed px-6 py-16 text-center text-body text-muted-foreground">
                  {t(($) => $.create.no_sources)}
                </div>
              ) : (
                <div className="mt-6 grid gap-3">
                  {sources.map((item) => {
                    const selected = item.id === sourceId;
                    return (
                      <button
                        key={item.id}
                        type="button"
                        onClick={() => chooseSource(item)}
                        className={cn(
                          "flex items-start gap-4 rounded-xl border p-4 text-left transition-colors hover:bg-muted/40",
                          selected && "border-foreground ring-1 ring-foreground",
                        )}
                      >
                        <span className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-muted">
                          {sourceType === "agent" ? <Bot className="size-5" /> : <Users className="size-5" />}
                        </span>
                        <span className="min-w-0 flex-1">
                          <span className="block text-body font-medium">{item.name}</span>
                          <span className="mt-1 block line-clamp-2 text-caption text-muted-foreground">{item.description}</span>
                        </span>
                        {selected ? <Check className="mt-2 size-5 shrink-0" /> : null}
                      </button>
                    );
                  })}
                </div>
              )}
            </div>
          ) : null}

          {step === 3 ? (
            <div>
              <h3 className="text-title font-medium">{t(($) => $.create.info_title)}</h3>
              <p className="mt-1 text-body text-muted-foreground">{t(($) => $.create.info_description)}</p>
              <div className="mt-6 grid gap-5">
                <div className="grid gap-1.5">
                  <Label htmlFor="template-name">{t(($) => $.create.name)}</Label>
                  <Input id="template-name" value={name} onChange={(event) => setName(event.target.value)} />
                </div>
                <div className="flex items-center gap-4 rounded-xl border p-4">
                  <AvatarUploadControl
                    value={imageURL}
                    variant={sourceType}
                    name={name}
                    size={72}
                    onUploaded={setImageURL}
                    onClear={() => setImageURL(null)}
                    ariaLabel={t(($) => $.create.image)}
                  />
                  <div>
                    <p className="text-body font-medium">{t(($) => $.create.image)}</p>
                    <p className="mt-1 text-caption text-muted-foreground">{t(($) => $.create.image_description)}</p>
                  </div>
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="template-description">{t(($) => $.create.introduction)}</Label>
                  <Textarea id="template-description" rows={5} value={description} onChange={(event) => setDescription(event.target.value)} placeholder={t(($) => $.create.introduction_placeholder)} />
                  <p className={cn("text-caption", descriptionLength < MIN_TEMPLATE_DESCRIPTION ? "text-warning" : "text-muted-foreground")}>{t(($) => $.create.description_minimum, { count: MIN_TEMPLATE_DESCRIPTION, current: descriptionLength })}</p>
                </div>
                <div className="grid gap-1.5">
                  <Label htmlFor="template-tags">{t(($) => $.create.tags)}</Label>
                  <Input id="template-tags" value={tags} onChange={(event) => setTags(event.target.value)} placeholder={t(($) => $.create.tags_placeholder)} />
                </div>
                <div className="grid gap-2">
                  <Label>{t(($) => $.create.visibility)}</Label>
                  <div className="grid gap-2 md:grid-cols-3">
                    {([
                      ["private", t(($) => $.create.private)],
                      ["workspace", t(($) => $.create.workspace)],
                      ["public", t(($) => $.create.public)],
                    ] as Array<[MarketplaceTemplateVisibility, string]>).map(([value, label]) => (
                      <button
                        key={value}
                        type="button"
                        onClick={() => setVisibility(value)}
                        className={cn("rounded-lg border px-3 py-3 text-left text-body", visibility === value && "border-foreground ring-1 ring-foreground")}
                      >
                        {label}
                      </button>
                    ))}
                  </div>
                </div>
              </div>
            </div>
          ) : null}

          {step === 4 ? (
            <div>
              <h3 className="text-title font-medium">{t(($) => $.create.confirm_title)}</h3>
              <p className="mt-1 text-body text-muted-foreground">{t(($) => $.create.confirm_description)}</p>
              <div className="mt-6 overflow-hidden rounded-xl border">
                <div className="flex items-start gap-4 border-b p-5">
                  <span className="flex size-12 shrink-0 items-center justify-center rounded-xl bg-muted">
                    {sourceType === "agent" ? <Bot className="size-6" /> : <Users className="size-6" />}
                  </span>
                  <div className="min-w-0">
                    <p className="text-title font-medium">{name}</p>
                    <p className="mt-1 text-body text-muted-foreground">{description}</p>
                    <div className="mt-3 flex flex-wrap gap-1.5">
                      {tags.split(/[,，]/).map((tag) => tag.trim()).filter(Boolean).slice(0, 8).map((tag) => <Badge key={tag} variant="secondary">{tag}</Badge>)}
                    </div>
                  </div>
                </div>
                <dl className="grid gap-4 p-5 text-body md:grid-cols-3">
                  <div><dt className="text-caption text-muted-foreground">{t(($) => $.create.summary_type)}</dt><dd className="mt-1 font-medium">{sourceType === "agent" ? t(($) => $.page.agent_type) : t(($) => $.page.squad_type)}</dd></div>
                  <div><dt className="text-caption text-muted-foreground">{t(($) => $.create.summary_source)}</dt><dd className="mt-1 font-medium">{source?.name}</dd></div>
                  <div><dt className="text-caption text-muted-foreground">{t(($) => $.create.summary_content)}</dt><dd className="mt-1 font-medium">{t(($) => $.card.agents, { count: sourceAgentCount })}{sourceSkillCount === null ? "" : ` · ${t(($) => $.card.skills, { count: sourceSkillCount })}`}</dd></div>
                </dl>
              </div>
              <div className="mt-4 flex gap-3 rounded-xl border border-success/30 bg-success/5 p-4 text-caption text-muted-foreground">
                <ShieldCheck className="size-5 shrink-0 text-success" />
                <span>{t(($) => $.apply.description)}</span>
              </div>
            </div>
          ) : null}
        </div>

        <div className="flex items-center justify-between border-t px-8 py-4">
          <Button type="button" variant="ghost" disabled={step === 1 || mutation.isPending} onClick={() => setStep((step - 1) as WizardStep)}>
            <ChevronLeft className="size-4" />
            {t(($) => $.create.previous)}
          </Button>
          {step < 4 ? (
            <Button type="button" disabled={!canContinue} onClick={() => setStep((step + 1) as WizardStep)}>
              {t(($) => $.create.continue)}
              <ChevronRight className="size-4" />
            </Button>
          ) : (
            <Button type="button" disabled={mutation.isPending} onClick={() => mutation.mutate()}>
              {mutation.isPending ? <Loader2 className="size-4 animate-spin" /> : null}
              {t(($) => $.create.submit)}
            </Button>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
