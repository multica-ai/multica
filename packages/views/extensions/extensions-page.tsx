"use client";

import {
  Children,
  isValidElement,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
} from "react";
import { useQuery } from "@tanstack/react-query";
import {
  AlertCircle,
  ArrowRight,
  Blocks,
  Bot,
  Check,
  CheckCircle2,
  ChevronDown,
  ChevronRight,
  Command,
  FileText,
  FileJson,
  Folder,
  FolderOpen,
  Loader2,
  PackageOpen,
  Sparkles,
  Upload,
  Users,
  WandSparkles,
  X,
  type LucideIcon,
} from "lucide-react";
import { ApiError } from "@multica/core/api";
import {
  PLATFORM_EXTENSION_MAX_IMPORT_BYTES,
  PlatformExtensionDocumentTooLargeError,
  extensionDetailOptions,
  extensionListOptions,
  useImportPlatformExtension,
  usePreviewPlatformExtension,
  useUpdatePlatformExtension,
  type PlatformExtensionDetail,
  type PlatformExtensionImportConfiguration,
  type PlatformExtensionManifest,
  type PlatformExtensionManifestAgent,
  type PlatformExtensionManifestCommand,
  type PlatformExtensionManifestSkill,
  type PlatformExtensionMapping,
  type PlatformExtensionPreview,
  type PlatformExtensionRuntime,
} from "@multica/core/extensions";
import { useWorkspaceId } from "@multica/core/hooks";
import {
  Alert,
  AlertDescription,
  AlertTitle,
} from "@multica/ui/components/ui/alert";
import { Button } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../i18n";
import {
  CollectionPageHeader,
  CollectionPageState,
} from "../layout/collection-page";

export interface ExtensionsPageProps {
  wsId?: string;
}

type ExtensionDetailView = "mapping" | "resources" | "import";
type ImportErrorReason = "file" | "size" | "runtime" | "version" | "archived" | "generic";
type ImportNotice =
  | { kind: "success"; idempotent: boolean }
  | { kind: "error"; reason: ImportErrorReason };
type PendingExtensionPreview = { document: Uint8Array; preview: PlatformExtensionPreview };
type EditableAgent = {
  source_key: string;
  name: string;
  leader: boolean;
  runtime: PlatformExtensionRuntime | null;
};
type ExtensionSourceFile = {
  id: string;
  path: string;
  content: string;
  binary: boolean;
  byteSize: number;
};
type ExtensionSourceResource = {
  kind: "agent" | "skill" | "command";
  name: string;
  files: ExtensionSourceFile[];
};
type ExtensionSourceTreeNode = {
  name: string;
  path: string;
  file?: ExtensionSourceFile;
  children: ExtensionSourceTreeNode[];
};

const INTERNAL_RESOURCE_NOTICE = "下列 Agent 与 Skills 都是该小队的内部资源，仅在小队编排与执行时生效，不会出现在全局“智能体”或“Skills”列表，也不能作为普通任务的直接分配对象。";
const IMPORT_ANIMATION_STORAGE_KEY = "multica:extension-import-animation";

export function ExtensionsPage({ wsId }: ExtensionsPageProps = {}) {
  return wsId ? <ExtensionsPageContent wsId={wsId} /> : <WorkspaceExtensionsPage />;
}

function WorkspaceExtensionsPage() {
  const wsId = useWorkspaceId();
  return <ExtensionsPageContent wsId={wsId} />;
}

function ExtensionsPageContent({ wsId }: { wsId: string }) {
  const { t } = useT("extensions");
  const fileInputRef = useRef<HTMLInputElement>(null);
  const [selectedID, setSelectedID] = useState<string | null>(null);
  const [pendingPreview, setPendingPreview] = useState<PendingExtensionPreview | null>(null);
  const [imported, setImported] = useState<PlatformExtensionDetail | null>(null);
  const [notice, setNotice] = useState<ImportNotice | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const listQuery = useQuery(extensionListOptions(wsId));
  const previewMutation = usePreviewPlatformExtension();
  const importMutation = useImportPlatformExtension(wsId);
  const updateMutation = useUpdatePlatformExtension(wsId);

  const releases = useMemo(() => {
    const listed = listQuery.data ?? [];
    return !imported || listed.some((item) => item.release.id === imported.release.id)
      ? listed
      : [imported, ...listed];
  }, [imported, listQuery.data]);
  const pendingID = pendingPreview ? previewID(pendingPreview.preview) : null;
  const activeID = selectedID ?? pendingID ?? releases[0]?.release.id ?? "";
  const previewIsActive = activeID === pendingID;
  const detailQuery = useQuery({
    ...extensionDetailOptions(wsId, previewIsActive ? "" : activeID),
    enabled: Boolean(activeID) && !previewIsActive,
  });
  const activeMapping = imported?.release.id === activeID ? imported : detailQuery.data;

  const handleFile = async (event: ChangeEvent<HTMLInputElement>) => {
    const input = event.currentTarget;
    const file = input.files?.[0];
    if (!file) return;
    setNotice(null);
    if (!file.name.toLowerCase().endsWith(".zip")) {
      setNotice({ kind: "error", reason: "file" });
      input.value = "";
      return;
    }
    try {
      const document = new Uint8Array(await file.arrayBuffer());
      if (document.byteLength > PLATFORM_EXTENSION_MAX_IMPORT_BYTES) {
        setNotice({ kind: "error", reason: "size" });
        return;
      }
      const preview = await previewMutation.mutateAsync(document);
      if (!preview) throw new Error("empty extension preview");
      setPendingPreview({ document, preview });
      setSelectedID(previewID(preview));
      setPickerOpen(false);
    } catch (error) {
      setNotice({ kind: "error", reason: importErrorReason(error) });
    } finally {
      input.value = "";
    }
  };

  const confirmImport = async (configuration: PlatformExtensionImportConfiguration) => {
    if (!pendingPreview) return;
    setNotice(null);
    try {
      const result = await importMutation.mutateAsync({
        document: pendingPreview.document,
        configuration,
      });
      if (!result) throw new Error("empty extension import");
      if (typeof window !== "undefined") {
        window.sessionStorage.setItem(IMPORT_ANIMATION_STORAGE_KEY, result.release.id);
      }
      setImported({
        ...result,
        manifest: pendingPreview.preview.manifest,
        available_runtimes: pendingPreview.preview.runtimes,
      });
      setPendingPreview(null);
      setSelectedID(result.release.id);
      setNotice({ kind: "success", idempotent: result.idempotent });
    } catch (error) {
      setNotice({ kind: "error", reason: importErrorReason(error) });
    }
  };

  const saveMapping = async (configuration: PlatformExtensionImportConfiguration) => {
    if (!activeMapping) return;
    const result = await updateMutation.mutateAsync({
      id: activeMapping.release.id,
      configuration,
    });
    if (result) setImported({
      ...result,
      manifest: readExtensionManifest(activeMapping),
      available_runtimes: "available_runtimes" in activeMapping ? activeMapping.available_runtimes : [],
    });
  };

  const busy = previewMutation.isPending || importMutation.isPending || updateMutation.isPending;

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <CollectionPageHeader
        icon={Blocks}
        title="Extensions"
        description="导入并管理平台能力"
        actions={
          <>
            <input
              ref={fileInputRef}
              type="file"
              accept=".zip,application/zip"
              aria-label={t(($) => $.import.file_label)}
              className="sr-only"
              onChange={handleFile}
            />
            <Button type="button" size="sm" onClick={() => setPickerOpen(true)} disabled={busy}>
              {previewMutation.isPending ? <Loader2 className="animate-spin" /> : <Upload />}
              导入 Extension
            </Button>
          </>
        }
      />

      <ImportPickerDialog
        open={pickerOpen}
        onOpenChange={setPickerOpen}
        onChoose={() => fileInputRef.current?.click()}
      />
      {notice ? <ImportFeedback notice={notice} /> : null}
      {busy ? <div className="px-4 pt-4 sm:px-6" role="status"><Alert><Loader2 className="animate-spin" /><AlertTitle>{previewMutation.isPending ? "正在读取 Extension" : importMutation.isPending ? "正在导入 Extension" : "正在保存更改"}</AlertTitle></Alert></div> : null}

      {listQuery.isLoading && !imported ? <CollectionPageState icon={Loader2} title={t(($) => $.states.loading)} role="status" /> : null}
      {listQuery.isError && !imported ? <CollectionPageState icon={AlertCircle} title={t(($) => $.states.list_error_title)} description={t(($) => $.states.list_error_description)} tone="destructive" role="alert" /> : null}
      {!listQuery.isLoading && !listQuery.isError && releases.length === 0 && !pendingPreview ? <CollectionPageState icon={FileJson} title={t(($) => $.states.empty_title)} description={t(($) => $.states.empty_description)} /> : null}
      {(releases.length > 0 || pendingPreview) ? (
        <main className="min-h-0 min-w-0 flex-1 overflow-y-auto px-4 pb-12 pt-6 sm:px-7">
          <div className="mx-auto w-full max-w-[1360px]">
            <header className="mb-4">
              <h1 className="text-base font-semibold">Extensions</h1>
              <p className="mt-1 text-caption text-muted-foreground">每个导入版本都会创建独立、可追溯的小队模板。</p>
            </header>
            <div className="grid min-w-0 gap-8 lg:grid-cols-[238px_minmax(0,1fr)]">
              <ImportHistory releases={releases} pendingPreview={pendingPreview?.preview} activeID={activeID} onSelect={setSelectedID} />
              {previewIsActive && pendingPreview ? <ExtensionDetail key={pendingID} preview={pendingPreview.preview} submitting={importMutation.isPending} onConfirm={confirmImport} onCancel={() => { setNotice(null); setPendingPreview(null); setSelectedID(releases[0]?.release.id ?? null); }} /> : activeMapping ? <ExtensionDetail key={activeMapping.release.id} mapping={activeMapping} submitting={updateMutation.isPending} onSave={saveMapping} /> : detailQuery.isLoading ? <CollectionPageState icon={Loader2} title={t(($) => $.states.detail_loading)} role="status" className="self-center" /> : <CollectionPageState icon={AlertCircle} title={t(($) => $.states.detail_error_title)} description={t(($) => $.states.detail_error_description)} tone="destructive" role="alert" className="self-center" />}
            </div>
          </div>
        </main>
      ) : null}
    </div>
  );
}

function ImportPickerDialog({ open, onOpenChange, onChoose }: { open: boolean; onOpenChange: (open: boolean) => void; onChoose: () => void }) {
  return <Dialog open={open} onOpenChange={onOpenChange}><DialogContent className="sm:max-w-md"><DialogHeader><DialogTitle>导入 Extension</DialogTitle><DialogDescription>选择 ZIP 压缩包。通过唯一 <strong>-e2e</strong> Command 校验后，将新建一个版本化小队模板。</DialogDescription></DialogHeader><button type="button" onClick={onChoose} className="grid min-h-36 w-full place-items-center rounded-xl border border-dashed border-brand/40 bg-brand/[0.03] p-6 text-center transition-colors hover:bg-brand/[0.07]"><span><Upload className="mx-auto mb-3 size-6 text-brand" /><b className="block text-sm">选择 .zip 压缩包</b><small className="mt-1 block text-caption text-muted-foreground">不会覆盖已有版本</small></span></button></DialogContent></Dialog>;
}

function ImportFeedback({ notice }: { notice: ImportNotice }) {
  const { t } = useT("extensions");
  if (notice.kind === "success") return <div className="px-4 pt-4 sm:px-6" aria-live="polite"><Alert><CheckCircle2 /><AlertTitle>{t(($) => $.import.success)}</AlertTitle>{notice.idempotent ? <AlertDescription>{t(($) => $.import.idempotent)}</AlertDescription> : null}</Alert></div>;
  const message = { file: t(($) => $.import.invalid_file), size: t(($) => $.import.too_large), runtime: t(($) => $.import.runtime_unavailable_title), version: t(($) => $.import.version_conflict_title), archived: t(($) => $.import.archived_version_title), generic: t(($) => $.import.generic_error) }[notice.reason];
  const description = notice.reason === "runtime" ? t(($) => $.import.runtime_unavailable_description) : null;
  return <div className="px-4 pt-4 sm:px-6" aria-live="assertive"><Alert variant="destructive"><AlertCircle /><AlertTitle>{message}</AlertTitle>{description ? <AlertDescription>{description}</AlertDescription> : null}</Alert></div>;
}

function ImportHistory({ releases, pendingPreview, activeID, onSelect }: { releases: PlatformExtensionMapping[]; pendingPreview?: PlatformExtensionPreview; activeID: string; onSelect: (id: string) => void }) {
  const groups = useMemo(() => {
    const grouped = new Map<string, PlatformExtensionMapping[]>();
    for (const release of releases) grouped.set(release.release.extension_key, [...(grouped.get(release.release.extension_key) ?? []), release]);
    return [...grouped.entries()];
  }, [releases]);
  const pending = pendingPreview ? { id: previewID(pendingPreview), version: pendingPreview.release.version } : undefined;
  const pendingIsNewExtension = pendingPreview && !groups.some(([key]) => key === pendingPreview.release.extension_key);
  return <aside className="min-w-0" data-testid="extension-history"><div className="mb-2 flex items-center justify-between px-0.5 text-caption font-semibold uppercase tracking-[0.1em] text-muted-foreground"><span>导入历史</span><span>{String(groups.length + Number(Boolean(pendingIsNewExtension))).padStart(2, "0")}</span></div>{pendingIsNewExtension && pendingPreview && pending ? <HistoryGroup extensionKey={pendingPreview.release.extension_key} pending={pending} activeID={activeID} onSelect={onSelect} /> : null}{groups.map(([extensionKey, versions]) => <HistoryGroup key={extensionKey} extensionKey={extensionKey} releases={versions} pending={pendingPreview?.release.extension_key === extensionKey ? pending : undefined} activeID={activeID} onSelect={onSelect} />)}</aside>;
}

function HistoryGroup({ extensionKey, releases = [], pending, activeID, onSelect }: { extensionKey: string; releases?: PlatformExtensionMapping[]; pending?: { id: string; version: string }; activeID: string; onSelect: (id: string) => void }) {
	const activeRelease = releases.some((release) => release.release.id === activeID);
	const [expanded, setExpanded] = useState(() => Boolean(pending) || activeRelease);
	useEffect(() => {
		if (pending || activeRelease) setExpanded(true);
	}, [activeRelease, pending?.id]);
	const visibleReleases = pending ? releases.filter((release) => release.release.version !== pending.version) : releases;
	return <section data-testid={`extension-history-group-${extensionKey}`} className={cn("mb-3 overflow-hidden rounded-lg border bg-background", expanded && "border-brand/20")}><button type="button" aria-expanded={expanded} aria-label={extensionKey} onClick={() => setExpanded((current) => !current)} className={cn("flex w-full items-center gap-2 px-3 py-2.5 text-left text-sm font-semibold", expanded && "bg-brand/[0.045]")}>{expanded ? <ChevronDown className="size-3.5" /> : <ChevronRight className="size-3.5 text-muted-foreground" />}<span className="truncate">{extensionKey}</span></button>{expanded ? <div>{pending ? <HistoryReleaseButton id={pending.id} version={pending.version} active={activeID === pending.id} pending onSelect={onSelect} /> : null}{visibleReleases.map((release) => <HistoryReleaseButton key={release.release.id} id={release.release.id} version={release.release.version} active={activeID === release.release.id} archived={release.squad.archived === true} onSelect={onSelect} />)}</div> : null}</section>;
}

function HistoryReleaseButton({ id, version, active, pending = false, archived = false, onSelect }: { id: string; version: string; active: boolean; pending?: boolean; archived?: boolean; onSelect: (id: string) => void }) {
	return <button type="button" aria-label={`v${version}`} aria-pressed={active} data-testid={active ? "active-extension-history" : undefined} onClick={() => onSelect(id)} className={cn("relative flex w-full items-center justify-between px-3 py-2 pl-8 text-left text-caption transition-colors hover:bg-muted/60", active && "font-semibold text-brand", active && "before:absolute before:left-4 before:top-1/2 before:size-1.5 before:-translate-y-1/2 before:rounded-full before:bg-brand")}><span>v{version}</span><time className={cn("text-caption font-normal text-muted-foreground", pending && "font-semibold text-amber-700")}>{pending ? "待确认" : archived ? "已归档" : active ? "当前" : "已导入"}</time></button>;
}

function ExtensionDetail({ preview, mapping, submitting, onConfirm, onSave, onCancel }: { preview?: PlatformExtensionPreview; mapping?: PlatformExtensionDetail | PlatformExtensionMapping; submitting: boolean; onConfirm?: (configuration: PlatformExtensionImportConfiguration) => void; onSave?: (configuration: PlatformExtensionImportConfiguration) => void; onCancel?: () => void }) {
  const { t } = useT("extensions");
  const pending = Boolean(preview);
  const release = preview?.release ?? mapping!.release;
  const manifest = preview ? preview.manifest as PlatformExtensionManifest : readExtensionManifest(mapping!);
  const initialSquadBaseName = preview?.squad_base_name ?? getSquadBaseName(mapping!.squad.name, release.version);
  const initialAgents: EditableAgent[] = preview ? preview.agents.map((agent) => ({ source_key: agent.source_key, name: agent.name, leader: agent.leader, runtime: preview.runtimes.find((runtime) => runtime.id === agent.runtime_id) ?? null })) : mapping!.agents;
  const initialRuntimeIDs = Object.fromEntries(initialAgents.map((agent) => [agent.source_key, agent.runtime?.id ?? ""]));
  const runtimes = uniqueRuntimes(preview?.runtimes ?? ("available_runtimes" in mapping! ? mapping!.available_runtimes : []), initialAgents.map((agent) => agent.runtime));
  const [view, setView] = useState<ExtensionDetailView>("mapping");
  const [squadBaseName, setSquadBaseName] = useState(initialSquadBaseName);
  const [agentRuntimeIDs, setAgentRuntimeIDs] = useState<Record<string, string>>(initialRuntimeIDs);
  const [sourceResource, setSourceResource] = useState<ExtensionSourceResource | null>(null);
  const releaseIdentity = "id" in release ? release.id : `preview:${release.digest}`;
  useEffect(() => { setView("mapping"); setSquadBaseName(initialSquadBaseName); setAgentRuntimeIDs(initialRuntimeIDs); }, [initialSquadBaseName, releaseIdentity]);
  const nameDirty = squadBaseName.trim() !== initialSquadBaseName;
  const changedAgentKeys = new Set(initialAgents.filter((agent) => (agentRuntimeIDs[agent.source_key] ?? "") !== (initialRuntimeIDs[agent.source_key] ?? "")).map((agent) => agent.source_key));
  const archived = mapping?.squad.archived ?? false;
  const dirty = !archived && (nameDirty || changedAgentKeys.size > 0);
  const config = { squad_base_name: squadBaseName.trim(), agent_runtime_ids: agentRuntimeIDs };
  const flowCommands = manifest.flow_commands ?? [];
  const generatedCommands = manifest.runtime_commands ?? [];
  const declaredSkills = (manifest.skills ?? mapping?.skills.map((skill) => ({ name: skill.name })) ?? []).map((skill) => skill.name).filter(Boolean);
  const squadName = `${squadBaseName.trim() || initialSquadBaseName} · v${release.version}`;
  const submit = () => { if (!config.squad_base_name || archived) return; pending ? onConfirm?.(config) : onSave?.(config); };

  return <section className="min-w-0"><header className="flex items-center justify-between gap-4 border-b pb-4"><div className="flex min-w-0 items-center gap-3"><span className="grid size-9 shrink-0 place-items-center rounded-lg border border-brand/15 bg-brand/[0.07] text-brand"><PackageOpen className="size-4" /></span><div className="min-w-0"><h2 className="truncate text-lg font-semibold">{release.extension_key}</h2><p className="mt-0.5 text-caption text-muted-foreground">Extension v{release.version} · Platform Agent CLI</p></div></div><span className={cn("shrink-0 rounded-md border px-2 py-1 text-caption font-semibold", pending ? "border-amber-200 bg-amber-50 text-amber-700" : archived ? "border-muted bg-muted text-muted-foreground" : "border-success/30 bg-success/5 text-success")}>{pending ? "待确认" : archived ? "已归档" : "已导入"}</span></header><nav className="flex gap-5 border-b" aria-label={t(($) => $.details.title)}>{([ ["mapping", t(($) => $.details.mapping)], ["resources", t(($) => $.details.resources)], ["import", t(($) => $.details.import)] ] as const).map(([tab, label]) => <button key={tab} type="button" role="tab" aria-selected={view === tab} onClick={() => setView(tab)} className={cn("relative py-3 text-caption font-semibold text-muted-foreground", view === tab && "text-foreground after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-foreground")}>{label}</button>)}</nav><div className="pt-4">{view === "mapping" ? <MappingPane pending={pending} archived={archived} releaseVersion={release.version} squadBaseName={squadBaseName} onSquadChange={setSquadBaseName} agents={initialAgents} runtimes={runtimes} agentRuntimeIDs={agentRuntimeIDs} onAgentRuntimeChange={(key, runtimeID) => setAgentRuntimeIDs((current) => ({ ...current, [key]: runtimeID }))} flowCommands={flowCommands.map((command) => command.name)} generatedCommands={generatedCommands.map((command) => command.name)} skills={declaredSkills} nameDirty={nameDirty} changedAgentKeys={changedAgentKeys} dirty={dirty} submitting={submitting} onSubmit={submit} onCancel={onCancel} /> : null}{view === "resources" ? <ResourceInventory squadName={squadName} agents={initialAgents.map((agent) => ({ ...agent, runtime: runtimes.find((runtime) => runtime.id === agentRuntimeIDs[agent.source_key]) ?? null }))} manifest={manifest} pending={pending} archived={archived} onViewSource={setSourceResource} /> : null}{view === "import" ? <ImportInformation release={release} squadName={squadName} pending={pending} archived={archived} /> : null}</div><ExtensionResourceSourceDialog resource={sourceResource} onClose={() => setSourceResource(null)} /></section>;
}

function MappingPane({ pending, archived, releaseVersion, squadBaseName, onSquadChange, agents, runtimes, agentRuntimeIDs, onAgentRuntimeChange, flowCommands, generatedCommands, skills, nameDirty, changedAgentKeys, dirty, submitting, onSubmit, onCancel }: { pending: boolean; archived: boolean; releaseVersion: string; squadBaseName: string; onSquadChange: (value: string) => void; agents: EditableAgent[]; runtimes: PlatformExtensionRuntime[]; agentRuntimeIDs: Record<string, string>; onAgentRuntimeChange: (key: string, runtimeID: string) => void; flowCommands: string[]; generatedCommands: string[]; skills: string[]; nameDirty: boolean; changedAgentKeys: Set<string>; dirty: boolean; submitting: boolean; onSubmit: () => void; onCancel?: () => void }) {
  const totalRows = flowCommands.length + generatedCommands.length + agents.length + skills.length;
  const [confirmedCount, setConfirmedCount] = useState(() => {
    if (pending || typeof window === "undefined") return Number.POSITIVE_INFINITY;
    return window.sessionStorage.getItem(IMPORT_ANIMATION_STORAGE_KEY) ? 0 : Number.POSITIVE_INFINITY;
  });

  useEffect(() => {
    if (pending || typeof window === "undefined" || !window.sessionStorage.getItem(IMPORT_ANIMATION_STORAGE_KEY)) {
      setConfirmedCount(Number.POSITIVE_INFINITY);
      return;
    }
    setConfirmedCount(0);
    let count = 0;
    const timer = window.setInterval(() => {
      count += 1;
      setConfirmedCount(count);
      if (count >= totalRows) {
        window.clearInterval(timer);
        window.sessionStorage.removeItem(IMPORT_ANIMATION_STORAGE_KEY);
      }
    }, 160);
    return () => window.clearInterval(timer);
  }, [pending, totalRows]);

  const isConfirmed = (index: number, valid = true) => !pending && valid && index < confirmedCount;
  const commandOffset = flowCommands.length;
  const agentOffset = commandOffset + generatedCommands.length;
  const skillOffset = agentOffset + agents.length;

  return (
    <div data-testid="extension-atomic-mapping">
      <Intro title="原子能力映射" status={pending ? "待确认" : archived ? "已归档" : "已导入"}>
        {pending
          ? "确认后创建新的版本化小队模板；不会覆盖已有版本。"
          : archived
            ? "此版本已归档；完整映射保留为只读。"
            : "此版本的资源映射已生效；可以修改小队基础名称及每个 Agent 的固定运行时。"}
      </Intro>
      <MappingGroup from="COMMAND" to="SQUAD INSTRUCTIONS" count={flowCommands.length}>
        {flowCommands.map((command, index) => (
          <MappingLine
            key={command}
            icon={Command}
            source={command}
            sourceMeta="Command"
            targetIcon={Users}
            target={(
              <>
                <label className="flex h-8 min-w-0 max-w-56 items-center gap-2 rounded-md border border-brand/45 bg-background px-2 text-caption outline-2 outline-brand/10 focus-within:outline-brand/30">
                  <span className="shrink-0 text-[11px] font-semibold text-muted-foreground">小队名称</span>
                  <input
                    aria-label="小队名称"
                    value={squadBaseName}
                    disabled={archived}
                    onChange={(event) => onSquadChange(event.target.value)}
                    className="min-w-0 flex-1 bg-transparent font-semibold outline-none disabled:cursor-not-allowed disabled:text-muted-foreground"
                  />
                </label>
                <span className="shrink-0 rounded-full border bg-muted/40 px-2 py-1 text-[11px] font-semibold text-muted-foreground">· v{releaseVersion}</span>
              </>
            )}
            targetName="Squad Instructions"
            confirmed={isConfirmed(index, archived || !nameDirty)}
          />
        ))}
      </MappingGroup>
      <MappingGroup from="COMMAND" to="INTERNAL SKILL" count={generatedCommands.length}>
        {generatedCommands.map((command, index) => (
          <MappingLine key={command} icon={Command} source={command} sourceMeta="Command" targetIcon={Command} targetName={command} targetMeta="内部 Skill" confirmed={isConfirmed(commandOffset + index)} />
        ))}
      </MappingGroup>
      <MappingGroup from="AGENT" to="INTERNAL AGENT" count={agents.length}>
        {agents.map((agent, index) => (
          <MappingLine
            key={agent.source_key}
            icon={Bot}
            source={agent.name}
            sourceMeta="Agent"
            targetIcon={Bot}
            targetName={agent.name}
            targetMeta="内部 Agent · 固定运行时"
            confirmed={isConfirmed(agentOffset + index, archived || !changedAgentKeys.has(agent.source_key))}
            target={(
              <select
                aria-label={`${getAgentDisplayName(agent.name)} runtime`}
                value={agentRuntimeIDs[agent.source_key] ?? ""}
                disabled={archived}
                onChange={(event) => onAgentRuntimeChange(agent.source_key, event.target.value)}
                className="h-8 min-w-36 max-w-full rounded-md border border-success/25 bg-background px-2 text-caption text-success outline-none disabled:cursor-not-allowed disabled:text-muted-foreground"
              >
                <option value="">未绑定运行时</option>
                {runtimes.map((runtime) => <option key={runtime.id} value={runtime.id}>{runtime.name}</option>)}
              </select>
            )}
          />
        ))}
      </MappingGroup>
      <MappingGroup from="SKILL" to="INTERNAL SKILL" count={skills.length}>
        {skills.map((skill, index) => (
          <MappingLine key={skill} icon={Sparkles} source={skill} sourceMeta="Skill" targetIcon={Sparkles} targetName={skill} targetMeta="内部 Skill" confirmed={isConfirmed(skillOffset + index)} />
        ))}
      </MappingGroup>
      <div className={cn("sticky bottom-3 mt-4 flex flex-wrap items-center justify-between gap-3 rounded-lg border bg-background/95 px-3 py-2.5 shadow-lg backdrop-blur", pending || (!archived && dirty) ? "" : "hidden")}>
        <span className="text-caption text-muted-foreground">{pending ? `确认后创建 ${squadBaseName || "小队"} · v${releaseVersion}。` : `将保存此版本模板的 ${Number(nameDirty) + changedAgentKeys.size} 项配置修改。`}</span>
        <div className="flex gap-2">
          {pending ? <Button type="button" size="sm" variant="outline" onClick={onCancel}><X />取消</Button> : null}
          <Button type="button" size="sm" disabled={submitting || !squadBaseName.trim() || (!pending && !dirty)} onClick={onSubmit}>
            {submitting ? <Loader2 className="animate-spin" /> : <CheckCircle2 />}{pending ? "确认导入" : "保存更改"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function MappingGroup({ from, to, count, children }: { from: string; to: string; count: number; children: ReactNode }) {
  if (count === 0) return null;
  return <section className="mt-3 overflow-hidden rounded-xl border bg-background"><header className="flex h-10 items-center justify-between border-b bg-muted/[0.18] px-3"><div className="flex items-center gap-2 text-[11px] font-extrabold tracking-[0.12em]"><span className="text-muted-foreground">{from}</span><ArrowRight className="size-4 text-brand" /><span className="text-success">{to}</span></div><span className="text-caption text-muted-foreground">{count} 条</span></header>{children}</section>;
}

function MappingLine({ icon, source, sourceMeta, targetIcon, targetName, targetMeta, target, confirmed }: { icon: LucideIcon; source: string; sourceMeta: string; targetIcon: LucideIcon; targetName?: string; targetMeta?: string; target?: ReactNode; confirmed: boolean }) {
  const runtimeUnavailable = targetMeta?.startsWith("内部 Agent")
    && isValidElement<{ children?: ReactNode }>(target)
    && Children.toArray(target.props.children).length <= 1;
  const configuration = runtimeUnavailable
    ? <span className="rounded-md border border-muted bg-muted/40 px-2 py-1 text-caption text-muted-foreground">暂无可用运行时</span>
    : target;

  return <article className="flex min-w-0 items-center gap-3 border-b px-3 py-2.5 last:border-0"><span data-testid="mapping-progress-indicator" data-state={confirmed ? "confirmed" : "pending"} className={cn("grid size-[18px] shrink-0 place-items-center rounded-full border text-[10px] transition-colors duration-200", confirmed ? "border-success/35 bg-success/10 text-success" : "border-muted-foreground/35 text-muted-foreground/50")}>{confirmed ? <Check className="size-3" /> : null}</span><div data-testid="mapping-source" className="w-[340px] max-w-[42%] shrink-0"><Entity icon={icon} name={source} meta={sourceMeta} /></div><ArrowRight data-testid="mapping-transfer-arrow" className="size-4 shrink-0 text-brand" /><div data-testid="mapping-target" className="ml-6 flex min-w-0 flex-1 items-center gap-2"><Entity icon={targetIcon} name={targetName ?? source} meta={targetMeta} output />{configuration ? <div data-testid="mapping-configuration" className="ml-auto flex min-w-0 shrink-0 items-center justify-end gap-2">{configuration}</div> : null}</div></article>;
}

function Entity({ icon: Icon, name, meta, output = false }: { icon: LucideIcon; name: string; meta?: string; output?: boolean }) {
  const displayName = meta === "Agent" || meta?.startsWith("内部 Agent")
    ? getAgentDisplayName(name)
    : name === "Squad Instructions · 此版本模板"
      ? "Squad Instructions"
      : name;
  return <div className={cn("flex min-w-0 items-center gap-2", output && "text-success")}><span className={cn("grid size-6 shrink-0 place-items-center rounded-md bg-muted text-muted-foreground", output && "bg-success/10 text-success")}><Icon className="size-3.5" /></span><span className="min-w-0"><strong className="block truncate text-caption">{displayName}</strong>{meta ? <small className="mt-0.5 block truncate text-[11px] text-muted-foreground">{meta}</small> : null}</span></div>;
}

function ResourceInventory({ squadName, agents, manifest, pending, archived, onViewSource }: { squadName: string; agents: EditableAgent[]; manifest: PlatformExtensionManifest; pending: boolean; archived: boolean; onViewSource: (resource: ExtensionSourceResource) => void }) {
  const sourceAgents = manifest.agents ?? [];
  const sourceSkills = manifest.skills ?? [];
  const commandSkills = manifest.runtime_commands ?? [];

  return <div data-testid="extension-resource-inventory">
    <Intro title="资源清单" status={pending ? "待确认" : archived ? "已归档" : "已导入"}>{INTERNAL_RESOURCE_NOTICE}</Intro>
    <div className="grid gap-3 sm:grid-cols-2">
      <ResourceBlock icon={Users} title="小队"><div className="text-base font-bold tracking-tight">{squadName}</div></ResourceBlock>
      <ResourceBlock icon={Bot} title="智能体" wide>
        <div className="grid gap-2">{agents.map((agent) => {
          const source = sourceAgents.find((candidate) => candidate.key === agent.source_key)
            ?? { key: agent.source_key, name: getAgentDisplayName(agent.name), prompt: "" };
          const resource = extensionAgentSource(source);
          return <button key={agent.source_key} type="button" aria-label={`View source for ${getAgentDisplayName(agent.name)}`} onClick={() => onViewSource(resource)} className="flex flex-wrap items-center justify-between gap-2 rounded-lg border bg-muted/[0.08] px-3 py-2 text-left transition-colors hover:border-brand/35 hover:bg-brand/[0.03]">
            <div className="min-w-0"><strong className="block truncate text-caption">{getAgentDisplayName(agent.name)}</strong><small className="mt-0.5 block text-[11px] text-muted-foreground">小队内部 Agent · 查看源文件</small></div>
            <span className={cn("rounded-full border px-2 py-1 text-[11px] font-semibold", agent.runtime ? "border-success/25 bg-success/5 text-success" : "border-muted bg-muted/40 text-muted-foreground")}>{agent.runtime ? `绑定运行时 · ${agent.runtime.name}` : "暂无可用运行时"}</span>
          </button>;
        })}</div>
      </ResourceBlock>
      <ResourceBlock icon={WandSparkles} title="Skills" wide>
        <div className="grid gap-2 sm:grid-cols-2">{sourceSkills.map((skill) => <SourceResourceButton key={`skill:${skill.key ?? skill.name}`} icon={Sparkles} resource={extensionSkillSource(skill)} onViewSource={onViewSource} />)}{commandSkills.map((command) => <SourceResourceButton key={`command:${command.name}`} icon={Command} resource={extensionCommandSource(command)} onViewSource={onViewSource} />)}</div>
      </ResourceBlock>
    </div>
  </div>;
}

function SourceResourceButton({ icon: Icon, resource, onViewSource }: { icon: LucideIcon; resource: ExtensionSourceResource; onViewSource: (resource: ExtensionSourceResource) => void }) {
  return <button type="button" aria-label={`View source for ${resource.name}`} onClick={() => onViewSource(resource)} className="flex min-w-0 items-center gap-2 rounded-lg border bg-background px-2.5 py-2 text-left text-caption font-semibold transition-colors hover:border-brand/35 hover:bg-brand/[0.03]"><Icon className="size-3 shrink-0 text-brand" /><span className="truncate">{resource.name}</span><ChevronRight className="ml-auto size-3 shrink-0 text-muted-foreground" /></button>;
}

function ExtensionResourceSourceDialog({ resource, onClose }: { resource: ExtensionSourceResource | null; onClose: () => void }) {
  const [selectedFileID, setSelectedFileID] = useState<string | null>(null);
  useEffect(() => { setSelectedFileID(resource ? defaultSourceFile(resource)?.id ?? null : null); }, [resource]);
  const selectedFile = resource?.files.find((file) => file.id === selectedFileID) ?? (resource ? defaultSourceFile(resource) : null);

  return <Dialog open={Boolean(resource)} onOpenChange={(open) => { if (!open) onClose(); }}>
    <DialogContent className="flex h-[min(82vh,860px)] w-[94vw] max-w-[calc(100vw-2rem)] flex-col gap-0 overflow-hidden p-0 sm:max-w-[1800px]">
      <DialogHeader className="border-b px-5 py-4 pr-12">
        <DialogTitle>{resource?.name ?? "源文件"}</DialogTitle>
        <DialogDescription>只读源文件 · {resource?.kind === "agent" ? "内部 Agent" : resource?.kind === "command" ? "命令生成的内部 Skill" : "内部 Skill"}</DialogDescription>
      </DialogHeader>
      {resource && selectedFile ? <div className="grid min-h-0 flex-1 lg:grid-cols-[330px_minmax(0,1fr)]">
        <SourceFileTree resource={resource} selectedFileID={selectedFile.id} onSelectFile={setSelectedFileID} />
        <section className="flex min-h-0 flex-col bg-background">
          <div className="border-b px-4 py-2.5 text-caption font-medium text-muted-foreground">{selectedFile.path}</div>
          {selectedFile.binary ? <div data-testid="extension-binary-file" className="grid flex-1 place-items-center p-6 text-center text-sm text-muted-foreground"><div><FileJson className="mx-auto mb-2 size-6" /><b className="block text-foreground">二进制文件</b><span className="mt-1 block">{formatByteSize(selectedFile.byteSize)}</span></div></div> : <pre data-testid="extension-source-content" className="min-h-0 flex-1 overflow-auto whitespace-pre bg-muted/[0.08] p-5 font-mono text-xs leading-6 text-foreground">{selectedFile.content || "（文件为空）"}</pre>}
        </section>
      </div> : <div className="grid flex-1 place-items-center p-6 text-sm text-muted-foreground">此资源没有可展示的文件。</div>}
    </DialogContent>
  </Dialog>;
}

function SourceFileTree({ resource, selectedFileID, onSelectFile }: { resource: ExtensionSourceResource; selectedFileID: string; onSelectFile: (id: string) => void }) {
  const tree = useMemo(() => buildSourceFileTree(resource.files), [resource.files]);
  const [collapsedDirectories, setCollapsedDirectories] = useState<Set<string>>(() => new Set());
  useEffect(() => { setCollapsedDirectories(new Set()); }, [resource]);
  const toggleDirectory = (path: string) => setCollapsedDirectories((current) => {
    const next = new Set(current);
    if (next.has(path)) next.delete(path); else next.add(path);
    return next;
  });

  return <nav aria-label="Source files" className="min-h-0 overflow-auto border-b bg-muted/[0.12] p-2 lg:border-b-0 lg:border-r"><div className="space-y-0.5">{tree.map((node) => <SourceFileTreeNode key={node.path} node={node} depth={0} selectedFileID={selectedFileID} collapsedDirectories={collapsedDirectories} onToggleDirectory={toggleDirectory} onSelectFile={onSelectFile} />)}</div></nav>;
}

function SourceFileTreeNode({ node, depth, selectedFileID, collapsedDirectories, onToggleDirectory, onSelectFile }: { node: ExtensionSourceTreeNode; depth: number; selectedFileID: string; collapsedDirectories: Set<string>; onToggleDirectory: (path: string) => void; onSelectFile: (id: string) => void }) {
  const isFile = Boolean(node.file);
  const expanded = !collapsedDirectories.has(node.path);
  const indentation = { paddingLeft: `${0.5 + depth * 0.9}rem` };
  if (isFile && node.file) return <button type="button" aria-label={node.path} aria-current={selectedFileID === node.file.id ? "page" : undefined} onClick={() => onSelectFile(node.file!.id)} style={indentation} className={cn("flex w-full items-center gap-2 rounded-md py-1.5 pr-2 text-left text-caption transition-colors", selectedFileID === node.file.id ? "bg-brand/[0.1] font-semibold text-brand" : "hover:bg-muted")}><FileText className="size-3.5 shrink-0" /><span className="truncate">{node.name}</span></button>;
  return <div><button type="button" aria-label={`Toggle folder ${node.name}`} aria-expanded={expanded} onClick={() => onToggleDirectory(node.path)} style={indentation} className="flex w-full items-center gap-1.5 rounded-md py-1.5 pr-2 text-left text-caption font-medium text-muted-foreground transition-colors hover:bg-muted hover:text-foreground">{expanded ? <ChevronDown className="size-3.5 shrink-0" /> : <ChevronRight className="size-3.5 shrink-0" />}{expanded ? <FolderOpen className="size-3.5 shrink-0 text-brand" /> : <Folder className="size-3.5 shrink-0 text-brand" />}<span className="truncate">{node.name}</span></button>{expanded ? <div>{node.children.map((child) => <SourceFileTreeNode key={child.path} node={child} depth={depth + 1} selectedFileID={selectedFileID} collapsedDirectories={collapsedDirectories} onToggleDirectory={onToggleDirectory} onSelectFile={onSelectFile} />)}</div> : null}</div>;
}

function extensionAgentSource(agent: PlatformExtensionManifestAgent): ExtensionSourceResource {
  const key = agent.key || agent.name;
  const content = agent.prompt ?? "";
  return { kind: "agent", name: agent.name, files: [{ id: `agent:${key}`, path: `agents/${key}.md`, content, binary: false, byteSize: textByteSize(content) }] };
}

function extensionSkillSource(skill: PlatformExtensionManifestSkill): ExtensionSourceResource {
  const key = skill.key || skill.name;
  return { kind: "skill", name: skill.name, files: [...(skill.files ?? [])].sort((left, right) => Number(right.path === "SKILL.md") - Number(left.path === "SKILL.md") || left.path.localeCompare(right.path)).map((file) => ({ id: `skill:${key}:${file.path}`, path: `skills/${key}/${file.path}`, content: file.content ?? "", binary: file.encoding === "base64", byteSize: file.encoding === "base64" ? base64ByteSize(file.content ?? "") : textByteSize(file.content ?? "") })) };
}

function extensionCommandSource(command: PlatformExtensionManifestCommand): ExtensionSourceResource {
  const content = command.content ?? "";
  return { kind: "command", name: command.name, files: [{ id: `command:${command.name}`, path: `commands/${command.name}.md`, content, binary: false, byteSize: textByteSize(content) }] };
}

function defaultSourceFile(resource: ExtensionSourceResource): ExtensionSourceFile | null {
  return resource.files.find((file) => file.path.endsWith("/SKILL.md")) ?? resource.files[0] ?? null;
}

function buildSourceFileTree(files: ExtensionSourceFile[]): ExtensionSourceTreeNode[] {
  const root: ExtensionSourceTreeNode = { name: "", path: "", children: [] };
  for (const file of files) {
    let parent = root;
    let path = "";
    const parts = file.path.split("/").filter(Boolean);
    for (const [index, name] of parts.entries()) {
      path = path ? `${path}/${name}` : name;
      let node = parent.children.find((candidate) => candidate.name === name);
      if (!node) {
        node = { name, path, children: [] };
        parent.children.push(node);
      }
      if (index === parts.length - 1) node.file = file;
      parent = node;
    }
  }
  sortSourceTreeNodes(root.children);
  return root.children;
}

function sortSourceTreeNodes(nodes: ExtensionSourceTreeNode[]): void {
  nodes.sort((left, right) => {
    if (Boolean(left.file) !== Boolean(right.file)) return left.file ? 1 : -1;
    if (left.name === "SKILL.md") return -1;
    if (right.name === "SKILL.md") return 1;
    return left.name.localeCompare(right.name);
  });
  for (const node of nodes) sortSourceTreeNodes(node.children);
}

function textByteSize(content: string): number {
  return new TextEncoder().encode(content).byteLength;
}

function base64ByteSize(content: string): number {
  const normalized = content.replace(/\s/g, "");
  if (!normalized) return 0;
  const padding = normalized.endsWith("==") ? 2 : normalized.endsWith("=") ? 1 : 0;
  return Math.max(0, Math.floor(normalized.length * 3 / 4) - padding);
}

function formatByteSize(bytes: number): string {
  return `${bytes} B`;
}

function Intro({ title, status, children }: { title: string; status: string; children: ReactNode }) {
  return <div className="mb-3 flex items-start justify-between gap-3 rounded-lg border border-brand/15 bg-brand/[0.025] px-3 py-2.5 text-caption leading-5 text-muted-foreground"><span><b className="text-foreground">{title}</b><br />{children}</span><span className={cn("shrink-0 font-semibold", status === "待确认" ? "text-amber-700" : status === "已归档" ? "text-muted-foreground" : "text-success")}>{status}</span></div>;
}

function ResourceBlock({ icon: Icon, title, children, wide = false }: { icon: LucideIcon; title: string; children: ReactNode; wide?: boolean }) {
  return <article className={cn("rounded-xl border bg-background p-4", wide && "sm:col-span-2")}><h3 className="mb-3 flex items-center gap-2 text-sm font-semibold"><span className="grid size-6 place-items-center rounded-md bg-muted text-muted-foreground"><Icon className="size-3.5" /></span>{title}</h3>{children}</article>;
}

function ImportInformation({ release, squadName, pending, archived }: { release: { extension_key: string; version: string; digest: string }; squadName: string; pending: boolean; archived: boolean }) {
  return <div className="rounded-xl border bg-background p-3"><InfoRow label="Squad 模板" value={squadName} /><InfoRow label="Extension release" value={`v${release.version}`} /><InfoRow label="成员副本" value="与模板隔离" /><InfoRow label="状态" value={pending ? "待确认" : archived ? "已归档" : "可用"} /></div>;
}

function InfoRow({ label, value }: { label: string; value: string }) {
  return <div className="flex items-center justify-between gap-4 border-b py-2.5 text-caption last:border-0"><span className="text-muted-foreground">{label}</span><b className="text-right">{value}</b></div>;
}

function isRuntimeUnavailable(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && !!error.body && typeof error.body === "object" && (error.body as { code?: unknown }).code === "PLATFORM_RUNTIME_UNAVAILABLE";
}

function isVersionConflict(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && !!error.body && typeof error.body === "object" && (error.body as { code?: unknown }).code === "EXTENSION_VERSION_IMMUTABLE";
}

function isArchivedVersion(error: unknown): boolean {
  return error instanceof ApiError && error.status === 409 && !!error.body && typeof error.body === "object" && (error.body as { code?: unknown }).code === "EXTENSION_VERSION_ARCHIVED";
}

function importErrorReason(error: unknown): ImportErrorReason {
  if (error instanceof PlatformExtensionDocumentTooLargeError) return "size";
  if (isRuntimeUnavailable(error)) return "runtime";
  if (isArchivedVersion(error)) return "archived";
  return isVersionConflict(error) ? "version" : "generic";
}

function previewID(preview: PlatformExtensionPreview): string {
  return `preview:${preview.release.extension_key}:${preview.release.version}:${preview.release.digest}`;
}

function readExtensionManifest(mapping: PlatformExtensionDetail | PlatformExtensionMapping): PlatformExtensionManifest {
  return "manifest" in mapping && mapping.manifest && typeof mapping.manifest === "object" && !Array.isArray(mapping.manifest) ? mapping.manifest as PlatformExtensionManifest : {};
}

function getSquadBaseName(name: string, version: string): string {
  const suffix = ` · v${version}`;
  return name.endsWith(suffix) ? name.slice(0, -suffix.length) : name;
}

function getAgentDisplayName(name: string): string {
  const separator = " / ";
  const index = name.lastIndexOf(separator);
  return index >= 0 ? name.slice(index + separator.length) : name;
}

function uniqueRuntimes(runtimes: PlatformExtensionRuntime[], selected: Array<PlatformExtensionRuntime | null>) {
  const result = new Map<string, PlatformExtensionRuntime>();
  for (const runtime of [...runtimes, ...selected.filter((runtime): runtime is PlatformExtensionRuntime => runtime !== null)]) result.set(runtime.id, runtime);
  return [...result.values()];
}
