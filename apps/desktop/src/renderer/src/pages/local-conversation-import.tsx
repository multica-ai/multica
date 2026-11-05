import { Slot } from "radix-ui";
import { useMemo } from "react";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useId, useRef, useState } from "react";
import { useInfiniteQuery, useQueryClient } from "@tanstack/react-query";
import { FileInput, FolderKanban, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Checkbox } from "@multica/ui/components/ui/checkbox";
import { Input } from "@multica/ui/components/ui/input";
import {
  Dialog,
  DialogTrigger,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter
} from "@multica/ui/components/ui/dialog";
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem
} from "@multica/ui/components/ui/dropdown-menu";
import { ProviderLogo } from "@multica/views/runtimes/provider-logo";
import type { LocalProviderId } from "@multica/core/local-conversation/protocol";
import { parseWithFallback } from "@multica/core/api/schema";
import {
  localV2ImportPageSchema,
  localV2ImportResultSchema,
  type LocalV2ImportPage,
  type LocalV2ImportResult,
  type LocalV2Scope,
  type LocalV2Transport
} from "@multica/core/local-conversation/v2";
import { conversationService } from "../platform/conversation-services";
import { observer } from "mobx-react-lite";
export const LocalConversationImportButton = observer(
  LocalConversationImportButtonRender
);

function LocalConversationImportButtonRender({
  scope,
  client,
  projects,
  chinese,
  onImported
}: {
  scope: LocalV2Scope;
  client: LocalV2Transport;
  projects: { id: string; title: string }[];
  chinese: boolean;
  onImported(): void;
}) {
  const [open, setOpen] = useState(false);
  const labelId = useId();
  const [provider, setProvider] = useState<LocalProviderId>("codex");
  const [selected, setSelected] = useState(new Set<string>());
  const [projectId, setProjectId] = useState<string | null>(null);
  const [search, setSearch] = useState("");
  const [busy, setBusy] = useState(false);
  const [selectingAll, setSelectingAll] = useState(false);
  const [progress, setProgress] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<LocalV2ImportResult | null>(null);
  const v2 = conversationService(client, scope);
  const cancelled = useRef(false);
  const qc = useQueryClient();
  const queryKey = [
    "local-native-history",
    scope.userId,
    scope.workspaceId,
    provider
  ];
  const history = useInfiniteQuery({
    queryKey,
    initialPageParam: undefined as string | undefined,
    queryFn: async ({ pageParam }) =>
      parseWithFallback<LocalV2ImportPage>(
        await v2.request(
          "GET",
          `/imports?provider=${provider}${pageParam ? `&cursor=${encodeURIComponent(pageParam)}` : ""}`
        ),
        localV2ImportPageSchema,
        { sessions: [], next_cursor: null },
        { endpoint: "local-v2.imports" }
      ),
    getNextPageParam: (page) => page.next_cursor ?? undefined,
    enabled: open,
    retry: false,
    staleTime: 0
  });
  const entries = [
    ...new Map(
      history.data?.pages
        .flatMap((page) => page.sessions)
        .map((entry) => [entry.id, entry]) ?? []
    ).values()
  ];
  const visible = entries.filter((entry) =>
    `${entry.title} ${entry.cwd}`
      .toLocaleLowerCase()
      .includes(search.toLocaleLowerCase())
  );
  const eligible = visible.filter((entry) => !entry.imported_id);
  const labels = useMemo(
    () =>
      chinese
        ? {
            title: "导入本机会话",
            description:
              "将 Codex 或 Claude Code 历史复制为独立会话，保留原会话。历史图片仅保留在原生上下文中。",
            source: "会话来源",
            project: "导入到项目",
            unassigned: "未分配项目",
            search: "搜索标题或工作目录",
            all: "选择全部历史",
            selectPage: "全选当前列表",
            more: "加载更多",
            empty: "没有找到本机会话",
            loading: "正在读取本机历史…",
            retry: "重试",
            imported: "已导入",
            import: "导入所选",
            importing: "正在导入",
            stop: "停止导入",
            close: "关闭",
            stopped: "已停止，已完成的导入已保留。",
            failed: "导入失败，请重试",
            done: "导入完成",
            selected: "已选择",
            scanned: "已读取",
            tooMany: "会话较多，请分批导入",
            noMatch: "没有匹配的会话"
          }
        : {
            title: "Import local conversations",
            description:
              "Copy Codex or Claude Code history into independent conversations. Original conversations stay intact; historical images remain in native context.",
            source: "Conversation source",
            project: "Import into project",
            unassigned: "No project",
            search: "Search title or working directory",
            all: "Select all history",
            selectPage: "Select current list",
            more: "Load more",
            empty: "No local conversations found",
            loading: "Reading local history…",
            retry: "Retry",
            imported: "Imported",
            import: "Import selected",
            importing: "Importing",
            stop: "Stop import",
            close: "Close",
            stopped: "Stopped. Completed imports were kept.",
            failed: "Could not import; retry",
            done: "Import complete",
            selected: "Selected",
            scanned: "Loaded",
            tooMany: "Import this history in smaller batches",
            noMatch: "No matching conversations"
          },
    [chinese]
  );
  const errorMessage = (value: unknown) =>
    value instanceof Error
      ? "kind" in value && value.kind === "http_404"
        ? chinese
          ? "重启 Multica 后可使用本机会话导入。"
          : "Restart Multica to load local conversation import."
        : value.message
      : labels.failed;
  const report = (value: unknown) => setError(errorMessage(value));
  const reportCallback = usePersistCallback(report);
  const toggle = (id: string) =>
    setSelected((previous) => {
      const next = new Set(previous);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  const toggleCallback = usePersistCallback(toggle);
  const selectAll = async () => {
    setSelectingAll(true);
    setError(null);
    cancelled.current = false;
    try {
      let data = history.data;
      const cursors = new Set<string>();
      while (!cancelled.current && data?.pages.at(-1)?.next_cursor) {
        const cursor = data.pages.at(-1)!.next_cursor!;
        if (cursors.has(cursor) || data.pages.length > 200)
          throw new Error(labels.tooMany);
        cursors.add(cursor);
        data = (await history.fetchNextPage({ throwOnError: true })).data;
      }
      setSelected(
        new Set(
          data?.pages
            .flatMap((page) => page.sessions)
            .filter((entry) => !entry.imported_id)
            .map((entry) => entry.id)
        )
      );
    } catch (value) {
      if (!cancelled.current) report(value);
    } finally {
      if (cancelled.current)
        setError(chinese ? "已停止读取历史。" : "Stopped reading history.");
      setSelectingAll(false);
    }
  };
  const startImport = async () => {
    setBusy(true);
    setProgress(0);
    setError(null);
    setResult(null);
    cancelled.current = false;
    const ids = [...selected],
      combined: LocalV2ImportResult = { imported: [], skipped: [], failed: [] };
    try {
      for (
        let offset = 0;
        offset < ids.length && !cancelled.current;
        offset += 25
      ) {
        const batch = localV2ImportResultSchema.parse(
          await v2.request("POST", "/imports", {
            provider,
            ids: ids.slice(offset, offset + 25),
            project_id: projectId
          })
        );
        combined.imported.push(...batch.imported);
        combined.skipped.push(...batch.skipped);
        combined.failed.push(...batch.failed);
        setProgress(Math.min(ids.length, offset + 25));
      }
      setResult(combined);
      setSelected(new Set(combined.failed.map((entry) => entry.source_id)));
      if (!cancelled.current && !combined.failed.length) {
        toast.success(
          chinese
            ? `已导入 ${combined.imported.length} 个会话${combined.skipped.length ? `，跳过 ${combined.skipped.length} 个重复会话` : ""}`
            : `Imported ${combined.imported.length} conversations${combined.skipped.length ? `; skipped ${combined.skipped.length} duplicates` : ""}`
        );
        setOpen(false);
      }
    } catch (value) {
      if (!cancelled.current) report(value);
    } finally {
      if (cancelled.current) setError(labels.stopped);
      setBusy(false);
      onImported();
      void qc.invalidateQueries({ queryKey });
    }
  };
  const disabled = busy || selectingAll;
  const onOpenChangeProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Dialog>["onOpenChange"]>
  >((value) => {
    if (!disabled) {
      setOpen(value);
      setError(null);
      setResult(null);
      setSelected(new Set());
    }
  });
  const onChangeProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onChange"]>
  >((event) => setSearch(event.target.value));
  const onValueChangeProp3 = usePersistCallback<
    NonNullable<
      React.ComponentProps<typeof DropdownMenuRadioGroup>["onValueChange"]
    >
  >((value) => setProjectId(value === "__none__" ? null : value));
  const onCheckedChangeProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Checkbox>["onCheckedChange"]>
  >((checked) =>
    setSelected((previous) => {
      const next = new Set(previous);
      eligible.forEach((entry) =>
        checked ? next.add(entry.id) : next.delete(entry.id)
      );
      return next;
    })
  );
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void selectAll();
  });
  const onClickProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void startImport();
  });
  return (
    <Dialog open={open} onOpenChange={onOpenChangeProp1}>
      <DialogTrigger render={<IconButton label={labels.title} />}>
        <FileInput />
      </DialogTrigger>
      <DialogContent className="sm:max-w-2xl" showCloseButton={!disabled}>
        <DialogHeader>
          <DialogTitle>{labels.title}</DialogTitle>
          <DialogDescription>{labels.description}</DialogDescription>
        </DialogHeader>
        <div role="group" aria-label={labels.source} className="flex gap-2">
          {(["codex", "claude"] as const).map((choice) => (
            <ButtonBinding1
              key={choice}
              choice={choice}
              provider={provider}
              disabled={disabled}
              setProvider={setProvider}
              setSelected={setSelected}
              setResult={setResult}
              setError={setError}
            />
          ))}
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <Input
            className="min-w-0 flex-1"
            aria-label={labels.search}
            placeholder={labels.search}
            value={search}
            disabled={disabled}
            onChange={onChangeProp2}
          />
          <DropdownMenu>
            <DropdownMenuTrigger
              render={
                <Button
                  variant="outline"
                  size="sm"
                  className="max-w-64"
                  disabled={disabled}
                  aria-label={labels.project}
                />
              }
            >
              <FolderKanban />
              <span className="truncate">
                {projects.find((project) => project.id === projectId)?.title ??
                  labels.unassigned}
              </span>
            </DropdownMenuTrigger>
            <DropdownMenuContent>
              <DropdownMenuRadioGroup
                value={projectId ?? "__none__"}
                onValueChange={onValueChangeProp3}
              >
                <DropdownMenuRadioItem value="__none__">
                  {labels.unassigned}
                </DropdownMenuRadioItem>
                {projects.map((project) => (
                  <DropdownMenuRadioItem key={project.id} value={project.id}>
                    {project.title}
                  </DropdownMenuRadioItem>
                ))}
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
        </div>
        <div className="flex flex-wrap items-center justify-between gap-2 text-caption text-muted-foreground">
          <label className="flex items-center gap-2">
            <Checkbox
              disabled={disabled || !eligible.length}
              checked={
                !!eligible.length &&
                eligible.every((entry) => selected.has(entry.id))
              }
              indeterminate={
                eligible.some((entry) => selected.has(entry.id)) &&
                !eligible.every((entry) => selected.has(entry.id))
              }
              onCheckedChange={onCheckedChangeProp4}
            />
            {labels.selectPage}
          </label>
          <span>
            {labels.scanned} {entries.length} · {labels.selected}{" "}
            {selected.size}
          </span>
          <Button
            size="xs"
            variant="ghost"
            disabled={disabled || history.isPending || history.isError}
            onClick={onClickProp5}
          >
            {selectingAll && <Loader2 className="animate-spin" />}
            {labels.all}
          </Button>
        </div>
        <div className="max-h-80 min-h-32 overflow-y-auto rounded-lg border p-1">
          {history.isPending ? (
            <p role="status" className="p-4 text-caption text-muted-foreground">
              {labels.loading}
            </p>
          ) : history.isError ? (
            <div role="alert" className="p-3 text-caption">
              <p>{errorMessage(history.error)}</p>
              <ButtonBinding2 history={history} labels={labels} />
            </div>
          ) : !visible.length ? (
            <p className="p-4 text-caption text-muted-foreground">
              {search ? labels.noMatch : labels.empty}
            </p>
          ) : (
            visible.map((entry) => (
              <label
                key={entry.id}
                className="flex cursor-pointer items-center gap-3 rounded-md p-2 hover:bg-muted"
              >
                <CheckboxBinding3
                  labelId={labelId}
                  entry={entry}
                  disabled={disabled}
                  selected={selected}
                  toggle={toggleCallback}
                />
                <span className="min-w-0 flex-1">
                  <span
                    id={`${labelId}-${entry.id}`}
                    className="block truncate text-label font-medium"
                    title={entry.title}
                  >
                    {entry.title}
                  </span>
                  <span
                    className="block truncate text-caption text-muted-foreground"
                    title={entry.cwd}
                  >
                    {entry.cwd}
                  </span>
                </span>
                <span className="shrink-0 text-micro text-muted-foreground">
                  {entry.imported_id
                    ? labels.imported
                    : new Date(entry.updated_at).toLocaleDateString(
                        chinese ? "zh-CN" : "en-US"
                      )}
                </span>
              </label>
            ))
          )}
          {history.hasNextPage && (
            <ButtonBinding4
              disabled={disabled}
              history={history}
              labels={labels}
            />
          )}
        </div>
        {error || result?.failed.length ? (
          <div
            role="alert"
            className="max-h-28 overflow-auto text-caption text-destructive"
          >
            {error}
            <ul>
              {result?.failed.map((failure) => (
                <li key={failure.source_id}>
                  {entries.find((entry) => entry.id === failure.source_id)
                    ?.title ?? failure.source_id}
                  : {failure.error}
                </li>
              ))}
            </ul>
          </div>
        ) : null}
        {busy && (
          <p role="status" className="text-caption text-muted-foreground">
            {labels.importing} {progress} / {selected.size}
          </p>
        )}
        <DialogFooter>
          {disabled ? (
            <ButtonBinding5
              cancelled={cancelled}
              v2={v2}
              report={reportCallback}
              labels={labels}
            />
          ) : (
            <ButtonBinding6 setOpen={setOpen} labels={labels} />
          )}
          <Button disabled={disabled || !selected.size} onClick={onClickProp6}>
            {busy && <Loader2 className="animate-spin" />}
            {labels.import} ({selected.size})
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  choice,
  provider,
  disabled,
  setProvider,
  setSelected,
  setResult,
  setError,
  ...slotProps
}: {
  choice: "codex" | "claude";
  provider: LocalProviderId;
  disabled: boolean;
  setProvider: import("react").Dispatch<
    import("react").SetStateAction<LocalProviderId>
  >;
  setSelected: import("react").Dispatch<
    import("react").SetStateAction<Set<string>>
  >;
  setResult: import("react").Dispatch<
    import("react").SetStateAction<{
      imported: { source_id: string; conversation_id: string; error: string }[];
      skipped: { source_id: string; conversation_id: string; error: string }[];
      failed: { source_id: string; conversation_id: string; error: string }[];
    } | null>
  >;
  setError: import("react").Dispatch<
    import("react").SetStateAction<string | null>
  >;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    choice: "codex" | "claude";
    provider: LocalProviderId;
    disabled: boolean;
    setProvider: import("react").Dispatch<
      import("react").SetStateAction<LocalProviderId>
    >;
    setSelected: import("react").Dispatch<
      import("react").SetStateAction<Set<string>>
    >;
    setResult: import("react").Dispatch<
      import("react").SetStateAction<{
        imported: {
          source_id: string;
          conversation_id: string;
          error: string;
        }[];
        skipped: {
          source_id: string;
          conversation_id: string;
          error: string;
        }[];
        failed: { source_id: string; conversation_id: string; error: string }[];
      } | null>
    >;
    setError: import("react").Dispatch<
      import("react").SetStateAction<string | null>
    >;
  }
>) {
  const onClickProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    setProvider(choice);
    setSelected(new Set());
    setResult(null);
    setError(null);
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        key={choice}
        size="sm"
        variant={provider === choice ? "secondary" : "ghost"}
        disabled={disabled}
        aria-pressed={provider === choice}
        onClick={onClickProp7}
      >
        <ProviderLogo provider={choice} className="size-4" />
        {choice === "codex" ? "Codex" : "Claude Code"}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  history,
  labels,
  ...slotProps
}: {
  history:
    | import("@tanstack/react-query").InfiniteQueryObserverRefetchErrorResult<
        import("@tanstack/react-query").InfiniteData<
          {
            sessions: {
              id: string;
              provider: string;
              title: string;
              cwd: string;
              created_at: string;
              updated_at: string;
              imported_id?: string | undefined;
            }[];
            next_cursor: string | null;
          },
          unknown
        >,
        Error
      >
    | import("@tanstack/react-query").InfiniteQueryObserverLoadingErrorResult<
        import("@tanstack/react-query").InfiniteData<
          {
            sessions: {
              id: string;
              provider: string;
              title: string;
              cwd: string;
              created_at: string;
              updated_at: string;
              imported_id?: string | undefined;
            }[];
            next_cursor: string | null;
          },
          unknown
        >,
        Error
      >;
  labels: {
    title: string;
    description: string;
    source: string;
    project: string;
    unassigned: string;
    search: string;
    all: string;
    selectPage: string;
    more: string;
    empty: string;
    loading: string;
    retry: string;
    imported: string;
    import: string;
    importing: string;
    stop: string;
    close: string;
    stopped: string;
    failed: string;
    done: string;
    selected: string;
    scanned: string;
    tooMany: string;
    noMatch: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    history:
      | import("@tanstack/react-query").InfiniteQueryObserverRefetchErrorResult<
          import("@tanstack/react-query").InfiniteData<
            {
              sessions: {
                id: string;
                provider: string;
                title: string;
                cwd: string;
                created_at: string;
                updated_at: string;
                imported_id?: string | undefined;
              }[];
              next_cursor: string | null;
            },
            unknown
          >,
          Error
        >
      | import("@tanstack/react-query").InfiniteQueryObserverLoadingErrorResult<
          import("@tanstack/react-query").InfiniteData<
            {
              sessions: {
                id: string;
                provider: string;
                title: string;
                cwd: string;
                created_at: string;
                updated_at: string;
                imported_id?: string | undefined;
              }[];
              next_cursor: string | null;
            },
            unknown
          >,
          Error
        >;
    labels: {
      title: string;
      description: string;
      source: string;
      project: string;
      unassigned: string;
      search: string;
      all: string;
      selectPage: string;
      more: string;
      empty: string;
      loading: string;
      retry: string;
      imported: string;
      import: string;
      importing: string;
      stop: string;
      close: string;
      stopped: string;
      failed: string;
      done: string;
      selected: string;
      scanned: string;
      tooMany: string;
      noMatch: string;
    };
  }
>) {
  const onClickProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void history.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button size="sm" variant="ghost" onClick={onClickProp8}>
        {labels.retry}
      </Button>
    </Slot.Root>
  );
});

const CheckboxBinding3 = observer(function CheckboxBinding3({
  labelId,
  entry,
  disabled,
  selected,
  toggle
}: {
  labelId: string;
  entry: {
    id: string;
    provider: string;
    title: string;
    cwd: string;
    created_at: string;
    updated_at: string;
    imported_id?: string | undefined;
  };
  disabled: boolean;
  selected: Set<string>;
  toggle: (id: string) => void;
}) {
  const onCheckedChangeProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Checkbox>["onCheckedChange"]>
  >(() => toggle(entry.id));
  return (
    <Checkbox
      aria-labelledby={`${labelId}-${entry.id}`}
      disabled={disabled || !!entry.imported_id}
      checked={!!entry.imported_id || selected.has(entry.id)}
      onCheckedChange={onCheckedChangeProp9}
    />
  );
});

const ButtonBinding4 = observer(function ButtonBinding4({
  disabled,
  history,
  labels,
  ...slotProps
}: {
  disabled: boolean;
  history: import("@tanstack/react-query").UseInfiniteQueryResult<
    import("@tanstack/react-query").InfiniteData<
      {
        sessions: {
          id: string;
          provider: string;
          title: string;
          cwd: string;
          created_at: string;
          updated_at: string;
          imported_id?: string | undefined;
        }[];
        next_cursor: string | null;
      },
      unknown
    >,
    Error
  >;
  labels: {
    title: string;
    description: string;
    source: string;
    project: string;
    unassigned: string;
    search: string;
    all: string;
    selectPage: string;
    more: string;
    empty: string;
    loading: string;
    retry: string;
    imported: string;
    import: string;
    importing: string;
    stop: string;
    close: string;
    stopped: string;
    failed: string;
    done: string;
    selected: string;
    scanned: string;
    tooMany: string;
    noMatch: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    disabled: boolean;
    history: import("@tanstack/react-query").UseInfiniteQueryResult<
      import("@tanstack/react-query").InfiniteData<
        {
          sessions: {
            id: string;
            provider: string;
            title: string;
            cwd: string;
            created_at: string;
            updated_at: string;
            imported_id?: string | undefined;
          }[];
          next_cursor: string | null;
        },
        unknown
      >,
      Error
    >;
    labels: {
      title: string;
      description: string;
      source: string;
      project: string;
      unassigned: string;
      search: string;
      all: string;
      selectPage: string;
      more: string;
      empty: string;
      loading: string;
      retry: string;
      imported: string;
      import: string;
      importing: string;
      stop: string;
      close: string;
      stopped: string;
      failed: string;
      done: string;
      selected: string;
      scanned: string;
      tooMany: string;
      noMatch: string;
    };
  }
>) {
  const onClickProp10 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void history.fetchNextPage();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="sm"
        disabled={disabled || history.isFetchingNextPage}
        onClick={onClickProp10}
      >
        {labels.more}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding5 = observer(function ButtonBinding5({
  cancelled,
  v2,
  report,
  labels,
  ...slotProps
}: {
  cancelled: import("react").RefObject<boolean>;
  v2: import("../../../../../../packages/conversation-client/services/conversation-service").ConversationService;
  report: (value: unknown) => void;
  labels: {
    title: string;
    description: string;
    source: string;
    project: string;
    unassigned: string;
    search: string;
    all: string;
    selectPage: string;
    more: string;
    empty: string;
    loading: string;
    retry: string;
    imported: string;
    import: string;
    importing: string;
    stop: string;
    close: string;
    stopped: string;
    failed: string;
    done: string;
    selected: string;
    scanned: string;
    tooMany: string;
    noMatch: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    cancelled: import("react").RefObject<boolean>;
    v2: import("../../../../../../packages/conversation-client/services/conversation-service").ConversationService;
    report: (value: unknown) => void;
    labels: {
      title: string;
      description: string;
      source: string;
      project: string;
      unassigned: string;
      search: string;
      all: string;
      selectPage: string;
      more: string;
      empty: string;
      loading: string;
      retry: string;
      imported: string;
      import: string;
      importing: string;
      stop: string;
      close: string;
      stopped: string;
      failed: string;
      done: string;
      selected: string;
      scanned: string;
      tooMany: string;
      noMatch: string;
    };
  }
>) {
  const onClickProp11 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    cancelled.current = true;
    void v2.request("POST", "/imports/cancel").catch(report);
  });
  return (
    <Slot.Root {...slotProps}>
      <Button variant="outline" onClick={onClickProp11}>
        {labels.stop}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding6 = observer(function ButtonBinding6({
  setOpen,
  labels,
  ...slotProps
}: {
  setOpen: import("react").Dispatch<import("react").SetStateAction<boolean>>;
  labels: {
    title: string;
    description: string;
    source: string;
    project: string;
    unassigned: string;
    search: string;
    all: string;
    selectPage: string;
    more: string;
    empty: string;
    loading: string;
    retry: string;
    imported: string;
    import: string;
    importing: string;
    stop: string;
    close: string;
    stopped: string;
    failed: string;
    done: string;
    selected: string;
    scanned: string;
    tooMany: string;
    noMatch: string;
  };
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setOpen: import("react").Dispatch<import("react").SetStateAction<boolean>>;
    labels: {
      title: string;
      description: string;
      source: string;
      project: string;
      unassigned: string;
      search: string;
      all: string;
      selectPage: string;
      more: string;
      empty: string;
      loading: string;
      retry: string;
      imported: string;
      import: string;
      importing: string;
      stop: string;
      close: string;
      stopped: string;
      failed: string;
      done: string;
      selected: string;
      scanned: string;
      tooMany: string;
      noMatch: string;
    };
  }
>) {
  const onClickProp12 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setOpen(false));
  return (
    <Slot.Root {...slotProps}>
      <Button variant="outline" onClick={onClickProp12}>
        {labels.close}
      </Button>
    </Slot.Root>
  );
});
