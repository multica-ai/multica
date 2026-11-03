"use client";

import { useState, type ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";
import { toast } from "sonner";
import {
  AlertCircle,
  Database,
  ExternalLink,
  Loader2,
  WifiOff,
} from "@multica/ui/icons";
import { Badge } from "@multica/ui/components/ui/badge";
import { Button } from "@multica/ui/components/ui/button";
import { Spinner } from "@multica/ui/components/ui/spinner";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@multica/ui/components/ui/tabs";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  knowledgeBasesOptions,
  knowledgeStatusOptions,
  useEnableKnowledge,
  type KnowledgeState,
} from "@multica/core/knowledge";
import { canManageKnowledge, useCurrentMember } from "@multica/core/permissions";
import { useCurrentWorkspace } from "@multica/core/paths";
import { CollectionPageHeader, CollectionPageState } from "../layout/collection-page";
import { useT } from "../i18n";
import { KnowledgeDocuments } from "./knowledge-documents";
import { KnowledgeDrafts } from "./knowledge-drafts";
import { KnowledgeGovernance } from "./knowledge-governance";
import { KnowledgeSearch } from "./knowledge-search";
import { KnowledgeUploadPanel } from "./knowledge-upload";
import { KnowledgeSetupPanel } from "./knowledge-setup";

type KnowledgeTab = "library" | "drafts" | "governance";

/**
 * The Knowledge page: one state dispatch over the knowledge service's state,
 * and, when it is ready, the library (search and documents) and the drafts
 * a person publishes. Owners and admins alone enable, retry, publish and
 * discard; the server enforces the same rule.
 */
export function KnowledgePage() {
  const { t } = useT("knowledge");
  const wsId = useWorkspaceId();
  const { userId, role } = useCurrentMember(wsId);
  const canManage = canManageKnowledge({ userId, role }).allowed;
  const [tab, setTab] = useState<KnowledgeTab>("library");
  const secretaryAgentId = useCurrentWorkspace()?.secretary_agent_id ?? null;

  const bases = useQuery(knowledgeBasesOptions(wsId));
  // The status carries the WeKnora link and is an owner and admin read, so
  // it is not requested for members at all.
  const status = useQuery({ ...knowledgeStatusOptions(wsId), enabled: canManage });
  const enable = useEnableKnowledge(wsId);

  const enableKnowledge = () =>
    enable.mutate(undefined, {
      onSuccess: () => toast.success(t(($) => $.toast.enabled)),
      onError: () => toast.error(t(($) => $.toast.enable_failed)),
    });

  const state: KnowledgeState = bases.data?.state ?? "unknown";
  const knowledgeBases = bases.data?.knowledge_bases ?? [];
  const uiUrl = canManage ? status.data?.ui_url ?? "" : "";

  const stateLabel = (value: KnowledgeState): string => {
    switch (value) {
      case "ready":
        return t(($) => $.state.ready);
      case "provisioning":
        return t(($) => $.state.provisioning);
      case "offline":
        return t(($) => $.state.offline);
      case "failed":
        return t(($) => $.state.failed);
      case "disabled":
        return t(($) => $.state.disabled);
      default:
        return t(($) => $.state.unknown);
    }
  };

  const header = (
    <CollectionPageHeader
      icon={Database}
      title={t(($) => $.title)}
      tools={
        bases.data?.offered ? (
          <Badge variant="outline" className="ml-1">
            {stateLabel(state)}
          </Badge>
        ) : null
      }
      actions={
        uiUrl ? (
          <Button
            variant="outline"
            size="sm"
            render={<a href={uiUrl} target="_blank" rel="noreferrer" />}
          >
            <ExternalLink />
            {t(($) => $.open_in_weknora)}
          </Button>
        ) : null
      }
    />
  );

  let body: ReactNode;
  if (bases.isPending) {
    body = (
      <div className="flex flex-1 items-center justify-center">
        <Spinner />
      </div>
    );
  } else if (bases.isError) {
    body = (
      <CollectionPageState
        role="alert"
        tone="destructive"
        icon={AlertCircle}
        title={t(($) => $.load_error.title)}
        actions={
          <Button variant="outline" size="sm" onClick={() => void bases.refetch()}>
            {t(($) => $.load_error.retry)}
          </Button>
        }
      />
    );
  } else if (!bases.data.offered) {
    body = (
      <CollectionPageState icon={Database} title={t(($) => $.not_offered.title)} />
    );
  } else if (state === "disabled") {
    body = canManage ? (
      <CollectionPageState
        icon={Database}
        title={t(($) => $.disabled.title)}
        actions={
          <Button onClick={enableKnowledge} disabled={enable.isPending} aria-busy={enable.isPending}>
            {t(($) => $.enable)}
          </Button>
        }
      />
    ) : (
      <CollectionPageState
        icon={Database}
        title={t(($) => $.disabled.title)}
        description={t(($) => $.disabled.description)}
      />
    );
  } else if (state === "provisioning") {
    body = (
      <CollectionPageState icon={Loader2} title={t(($) => $.provisioning.title)} />
    );
  } else if (state === "failed") {
    body = (
      <CollectionPageState
        icon={AlertCircle}
        tone="destructive"
        title={t(($) => $.failed.title)}
        actions={
          canManage ? (
            <Button onClick={enableKnowledge} disabled={enable.isPending} aria-busy={enable.isPending}>
              {t(($) => $.retry)}
            </Button>
          ) : null
        }
      />
    );
  } else if (state === "offline") {
    body = (
      <CollectionPageState
        icon={WifiOff}
        title={t(($) => $.offline.title)}
        description={t(($) => $.offline.description)}
      />
    );
  } else if (state === "ready") {
    body = (
      <div className="flex min-h-0 flex-1 flex-col gap-4 px-6 py-4">
        <Tabs value={tab} onValueChange={(value) => setTab(value as KnowledgeTab)}>
          <TabsList variant="line">
            <TabsTrigger value="library">{t(($) => $.tabs.library)}</TabsTrigger>
            <TabsTrigger value="drafts">{t(($) => $.tabs.drafts)}</TabsTrigger>
            {canManage ? <TabsTrigger value="setup">{t(($) => $.tabs.setup)}</TabsTrigger> : null}
            {canManage ? <TabsTrigger value="governance">{t(($) => $.tabs.governance)}</TabsTrigger> : null}
          </TabsList>
          <TabsContent value="library" className="flex flex-col gap-6 pt-4">
            <KnowledgeSearch
              wsId={wsId}
              knowledgeBases={knowledgeBases}
              secretaryAgentId={secretaryAgentId}
            />
            <KnowledgeUploadPanel wsId={wsId} knowledgeBases={knowledgeBases} />
            <KnowledgeDocuments wsId={wsId} knowledgeBases={knowledgeBases} />
          </TabsContent>
          <TabsContent value="drafts" className="pt-4">
            <KnowledgeDrafts
              wsId={wsId}
              knowledgeBases={knowledgeBases}
              canManage={canManage}
            />
          </TabsContent>
          {canManage ? (
            <TabsContent value="setup" className="pt-4">
              <KnowledgeSetupPanel wsId={wsId} knowledgeBases={knowledgeBases} />
            </TabsContent>
          ) : null}
          {canManage ? (
            <TabsContent value="governance" className="pt-4">
              <KnowledgeGovernance wsId={wsId} knowledgeBases={knowledgeBases} />
            </TabsContent>
          ) : null}
        </Tabs>
      </div>
    );
  } else {
    body = <CollectionPageState icon={AlertCircle} title={stateLabel(state)} />;
  }

  return (
    <div className="flex flex-1 min-h-0 flex-col">
      {header}
      {body}
    </div>
  );
}
