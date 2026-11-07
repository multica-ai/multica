import { Slot } from "radix-ui";
import { usePersistCallback } from "@multica/ui/hooks/use-persist-callback";
import { useEffect, useMemo, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import {
  ArrowLeft,
  ArrowRight,
  ChevronDown,
  ChevronRight,
  Code2,
  Copy,
  Eye,
  ExternalLink,
  File,
  Folder,
  Globe,
  Loader2,
  RefreshCw
} from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { IconButton } from "@multica/ui/components/common/icon-button";
import { Input } from "@multica/ui/components/ui/input";
import { Markdown } from "@multica/ui/markdown/Markdown";
import { copyText } from "@multica/ui/lib/clipboard";
import type {
  LocalConversationScope,
  LocalPreviewLocation
} from "@multica/core/local-conversation/protocol";
import type {
  WebPreviewState,
  WebPreviewCommand
} from "../../../shared/resource-preview";
import { ChromeProfileImport } from "./chrome-profile-import";
import { localV2Preview } from "../platform/local-v2-preview";
import { useLocalDevice } from "../platform/local-device";
import { isServicePreviewUrl } from "../../../shared/service-preview-url";
import { observer } from "mobx-react-lite";
export const PreviewFolder = observer(PreviewFolderRender);
export const LocalFilePreview = observer(LocalFilePreviewRender);
export const WebResourcePreview = observer(WebResourcePreviewRender);

function servicePreviewError(value: string | undefined, chinese: boolean) {
  const messages: Record<string, [string, string]> = {
    preview_unsupported: [
      "对方设备版本不支持服务预览，请更新后重试。",
      "Update the other device to use service preview."
    ],
    service_sharing_disabled: [
      "对方设备尚未开启服务共享，请在对方设备的设置中开启。",
      "Enable service sharing in the other device's settings."
    ],
    port_not_allowed: [
      "对方设备尚未共享此端口，请在对方设备的设置中添加。",
      "Add this port to the other device's shared service ports."
    ],
    target_unreachable: [
      "无法连接对方的本地服务，请确认服务已启动。",
      "Start the local service on the other device, then retry."
    ],
    device_unreachable: [
      "与对方设备的连接已断开，请检查设备是否在线后重试。",
      "Check that the other device is online, then retry."
    ],
    service_expired: [
      "服务访问已过期，请重试。",
      "Service access expired. Retry to reconnect."
    ],
    preview_destination_denied: [
      "此地址不支持预览，请检查网页地址。",
      "This address cannot be previewed. Check the webpage address."
    ],
    preview_connection_limited: [
      "远端预览连接数已达上限，请关闭部分网页后重试。",
      "Close some remote previews, then retry."
    ],
    preview_proxy_unavailable: [
      "预览连接不可用，请重试。",
      "The preview connection is unavailable. Retry to reconnect."
    ]
  };
  const message = messages[value?.split(":")[0] ?? ""];
  return message ? message[chinese ? 0 : 1] : value;
}

function PreviewFolderRender({
  location,
  selected,
  scope,
  sessionId,
  onSelect,
  chinese,
  defaultExpanded = true
}: {
  location: LocalPreviewLocation;
  selected?: string;
  scope: LocalConversationScope;
  sessionId?: string;
  onSelect: (file: LocalPreviewLocation) => void;
  chinese: boolean;
  defaultExpanded?: boolean;
}) {
  const device = useLocalDevice();
  const [expanded, setExpanded] = useState(defaultExpanded);
  const query = useQuery({
    queryKey: [
      "resource-folder",
      device.id,
      scope.userId,
      scope.workspaceId,
      location.root,
      location.path
    ],
    queryFn: () =>
      localV2Preview(device.transport, scope).list(location, sessionId),
    enabled: expanded
  });
  const onClickProp1 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setExpanded(!expanded));
  return (
    <div role="group" className="min-w-0">
      <Button
        variant="ghost"
        size="sm"
        className="w-full justify-start"
        aria-expanded={expanded}
        onClick={onClickProp1}
      >
        {expanded ? (
          <ChevronDown className="size-3.5 shrink-0" />
        ) : (
          <ChevronRight className="size-3.5 shrink-0" />
        )}
        <Folder className="size-4 shrink-0" />
        <span className="truncate">{location.name}</span>
      </Button>
      {expanded && (
        <div className="ml-3 border-l pl-1">
          {query.isPending ? (
            <Loader2 className="m-2 size-4 animate-spin" />
          ) : query.isError ? (
            <ButtonBinding1 query={query} chinese={chinese} />
          ) : (
            query.data.map((entry) =>
              entry.kind === "directory" ? (
                <PreviewFolder
                  defaultExpanded={false}
                  key={entry.path}
                  location={entry}
                  selected={selected}
                  scope={scope}
                  sessionId={sessionId}
                  chinese={chinese}
                  onSelect={onSelect}
                />
              ) : (
                <ButtonBinding2
                  key={entry.path}
                  entry={entry}
                  selected={selected}
                  onSelect={onSelect}
                />
              )
            )
          )}
          {query.data?.length === 1000 && (
            <p className="p-2 text-caption text-muted-foreground">
              {chinese ? "仅显示前 1000 项" : "Showing the first 1,000 items"}
            </p>
          )}
        </div>
      )}
    </div>
  );
}

function LocalFilePreviewRender({
  location,
  scope,
  sessionId,
  line,
  chinese
}: {
  location: LocalPreviewLocation;
  scope: LocalConversationScope;
  sessionId?: string;
  line?: number;
  chinese: boolean;
}) {
  const device = useLocalDevice();
  const previewKey = JSON.stringify([
    device.id,
    scope.userId,
    scope.workspaceId,
    location.root,
    location.path,
    line
  ]);
  const [mode, setMode] = useState({ key: previewKey, source: !!line });
  if (mode.key !== previewKey) setMode({ key: previewKey, source: !!line });
  const source = mode.key === previewKey ? mode.source : !!line;
  const query = useQuery({
    queryKey: [
      "resource-file",
      device.id,
      scope.userId,
      scope.workspaceId,
      location.root,
      location.path
    ],
    queryFn: () =>
      localV2Preview(device.transport, scope).read(location, sessionId)
  });
  const highlight = useRef<HTMLSpanElement>(null);
  useEffect(() => {
    highlight.current?.scrollIntoView({ block: "center" });
  }, [line, query.data, source]);
  const file = query.data;
  const markdown = /\.(md|markdown)$/i.test(location.name);
  const lines = useMemo(() => (file?.text ?? "").split("\n"), [file?.text]);
  const previewLines = useMemo(() => lines.slice(0, 5000), [lines]);
  return (
    <div className="flex h-full min-h-0 min-w-0 flex-col">
      <div className="flex h-10 shrink-0 items-center gap-2 border-b px-3 text-caption text-muted-foreground">
        <span className="min-w-0 flex-1 truncate" title={location.path}>
          {location.path.replace(
            location.root,
            location.root.split(/[\\/]/).at(-1) ?? ""
          )}
        </span>
        {file && (markdown || file.mimeType === "text/html") && (
          <ButtonBinding3
            setMode={setMode}
            previewKey={previewKey}
            source={source}
            chinese={chinese}
          />
        )}
        {file?.text && <IconButtonBinding4 chinese={chinese} file={file} />}
      </div>
      {query.isPending ? (
        <div role="status" className="flex flex-1 items-center justify-center">
          <Loader2 className="size-5 animate-spin" />
        </div>
      ) : query.isError ? (
        <div role="alert" className="p-4 text-body">
          <p>{query.error.message}</p>
          <ButtonBinding5 query={query} chinese={chinese} />
        </div>
      ) : (
        file && (
          <>
            {file.url && file.mimeType.startsWith("image/") ? (
              <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-4">
                <img
                  draggable={false}
                  alt={file.name}
                  className="max-h-full max-w-full object-contain"
                  src={file.url}
                />
              </div>
            ) : file.url && file.mimeType === "application/pdf" ? (
              <iframe
                className="min-h-0 w-full flex-1 border-0"
                title={file.name}
                src={file.url}
              />
            ) : file.mimeType === "text/html" && !source ? (
              <iframe
                className="min-h-0 w-full flex-1 border-0 bg-white"
                title={file.name}
                sandbox="allow-scripts"
                srcDoc={`<meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data: https:; style-src 'unsafe-inline'; script-src 'unsafe-inline'; base-uri 'none'; form-action 'none'; connect-src 'none'">${file.text ?? ""}`}
              />
            ) : markdown && !source ? (
              <div className="min-h-0 min-w-0 flex-1 overflow-auto p-4 text-body">
                <MarkdownBinding6 previewLines={previewLines} />
              </div>
            ) : (
              <pre className="min-h-0 flex-1 overflow-auto py-3 text-label leading-6">
                <code>
                  {previewLines.map((text, index) => (
                    <span
                      key={index}
                      ref={line === index + 1 ? highlight : undefined}
                      className={`flex min-w-max ${line === index + 1 ? "bg-accent" : ""}`}
                    >
                      <span
                        aria-hidden="true"
                        className="sticky left-0 w-12 shrink-0 select-none bg-background pr-3 text-right text-muted-foreground"
                      >
                        {index + 1}
                      </span>
                      <span className="pr-4">{text || " "}</span>
                    </span>
                  ))}
                </code>
              </pre>
            )}
            <div className="flex shrink-0 justify-between border-t px-3 py-2 text-caption text-muted-foreground">
              <span>
                {chinese ? "只读预览" : "Read-only preview"}
                {lines.length > 5000
                  ? chinese
                    ? " · 仅显示前 5000 行"
                    : " · First 5,000 lines shown"
                  : ""}
                {file.truncated
                  ? chinese
                    ? " · 仅显示前 1 MB"
                    : " · First 1 MB shown"
                  : ""}
              </span>
              <span>
                {line
                  ? chinese
                    ? `第 ${line} 行`
                    : `Line ${line}`
                  : `${Math.ceil(file.size / 1024)} KB`}
              </span>
            </div>
          </>
        )
      )}
    </div>
  );
}

function WebResourcePreviewRender({
  id,
  url,
  scope: owner,
  conversationId,
  active,
  chinese,
  onChange,
  onReference
}: {
  id: string;
  url: string;
  scope: LocalConversationScope;
  conversationId?: string;
  active: boolean;
  chinese: boolean;
  onChange: (url: string, title?: string, loading?: boolean) => void;
  onReference: (url: string, title: string) => void;
}) {
  const scope = useMemo(
    () => ({ userId: owner.userId, workspaceId: owner.workspaceId }),
    [owner.userId, owner.workspaceId]
  );
  const client = window.desktopAPI.resourcePreview;
  const device = useLocalDevice();
  const target = useMemo(
    () =>
      device.id !== "local" && conversationId
        ? { deviceId: device.id, conversationId }
        : undefined,
    [device.id, conversationId]
  );
  const elsewhere = (_value: string) => device.id !== "local" && !target;
  const elsewhereMessage = chinese
    ? "请先选择来源会话，再打开远端网页。"
    : "Select the source conversation before opening a remote webpage.";
  const [state, setState] = useState<WebPreviewState>({
    id,
    url,
    title: url,
    loading: true,
    canGoBack: false,
    canGoForward: false
  });
  const [address, setAddress] = useState(url);
  const [error, setError] = useState("");
  const content = useRef<HTMLDivElement>(null);
  const alive = useRef(true);
  const current = useRef({ url, active, onChange, elsewhere: false });
  current.current = { url, active, onChange, elsewhere: elsewhere(url) };
  const command = async (value: Omit<WebPreviewCommand, "id">) => {
    if (
      "url" in value &&
      typeof value.url === "string" &&
      elsewhere(value.url)
    ) {
      setError(elsewhereMessage);
      return;
    }
    try {
      const result = await client.request(scope, {
        ...value,
        id,
        ...(target ? { target } : {})
      });
      if (result && alive.current) setState(result);
    } catch (error) {
      if (alive.current)
        setError(error instanceof Error ? error.message : String(error));
    }
  };
  const commandCallback = usePersistCallback(command);
  useEffect(() => {
    alive.current = true;
    const release = client.subscribe((value) => {
      if (value.id !== id) return;
      setState(value);
      if (value.url) {
        setAddress(value.url);
        current.current.onChange(value.url, value.title, value.loading);
      }
    });
    return () => {
      alive.current = false;
      release();
      void client.request(scope, { id, action: "close" }).catch(() => {});
    };
  }, [client, id, scope]);
  useEffect(() => {
    const element = content.current;
    if (!element) return;
    let frame = 0;
    const update = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const box = element.getBoundingClientRect();
        const overlay = document.querySelector(
          '[role="dialog"],[role="menu"],[role="listbox"],[data-side-dragging="true"]'
        );
        if (
          !current.current.active ||
          overlay ||
          document.visibilityState !== "visible" ||
          current.current.elsewhere
        ) {
          void client.request(scope, { id, action: "hide" }).catch(() => {});
          return;
        }
        void client
          .request(scope, {
            id,
            action: "show",
            url: current.current.url,
            ...(target ? { target } : {}),
            bounds: {
              x: Math.max(0, box.x),
              y: Math.max(0, box.y),
              width: box.width,
              height: box.height
            }
          })
          .then((value) => {
            if (value && alive.current) setState(value);
          })
          .catch((error: unknown) => {
            if (alive.current)
              setError(error instanceof Error ? error.message : String(error));
          });
      });
    };
    const resize = new ResizeObserver(update);
    resize.observe(element);
    const mutation = new MutationObserver(update);
    mutation.observe(document.body, {
      childList: true,
      subtree: true,
      attributes: true,
      attributeFilter: ["data-side-dragging", "role", "open"]
    });
    window.addEventListener("resize", update);
    document.addEventListener("visibilitychange", update);
    update();
    return () => {
      cancelAnimationFrame(frame);
      resize.disconnect();
      mutation.disconnect();
      window.removeEventListener("resize", update);
      document.removeEventListener("visibilitychange", update);
      void client.request(scope, { id, action: "hide" }).catch(() => {});
    };
  }, [client, id, active, scope, target]);
  const onSubmitProp2 = usePersistCallback<
    NonNullable<React.ComponentProps<"form">["onSubmit"]>
  >((event) => {
    event.preventDefault();
    setError("");
    const value = /^https?:\/\//i.test(address)
      ? address
      : `https://${address}`;
    onChange(value);
    void command({ action: "navigate", url: value });
  });
  const onClickProp3 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    void command({ action: "back" });
  });
  const onClickProp4 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    void command({ action: "forward" });
  });
  const onClickProp5 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    setError("");
    void command({ action: "reload" });
  });
  const onChangeProp6 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Input>["onChange"]>
  >((event) => setAddress(event.target.value));
  const onClickProp7 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    void window.desktopAPI.openExternal(state.url || url);
  });
  const onClickProp8 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onReference(state.url || url, state.title || url));
  const onClickProp9 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void window.desktopAPI.openExternal(state.url || url);
  });
  return (
    <div className="flex h-full min-h-0 flex-col">
      <form
        className="flex h-11 shrink-0 items-center gap-1 border-b px-2"
        onSubmit={onSubmitProp2}
      >
        <IconButton
          label={chinese ? "网页后退" : "Back webpage"}
          disabled={!state.canGoBack}
          onClick={onClickProp3}
        >
          <ArrowLeft />
        </IconButton>
        <IconButton
          label={chinese ? "网页前进" : "Forward webpage"}
          disabled={!state.canGoForward}
          onClick={onClickProp4}
        >
          <ArrowRight />
        </IconButton>
        <IconButton
          label={chinese ? "刷新网页" : "Reload webpage"}
          onClick={onClickProp5}
        >
          {state.loading ? <Loader2 className="animate-spin" /> : <RefreshCw />}
        </IconButton>
        <Input
          aria-label={chinese ? "网页地址" : "Webpage address"}
          value={address}
          onChange={onChangeProp6}
          className="min-w-0 flex-1 text-caption"
        />
        <IconButton
          disabled={
            device.id !== "local" && isServicePreviewUrl(state.url || url)
          }
          label={chinese ? "在浏览器中打开" : "Open in browser"}
          onClick={onClickProp7}
        >
          <ExternalLink />
        </IconButton>
      </form>
      <div
        ref={content}
        className="relative min-h-0 flex-1 bg-background"
        data-web-preview={id}
      >
        {error || state.error || elsewhere(url) ? (
          <div role="alert" className="p-4 text-body">
            <p>
              {servicePreviewError(
                error || (elsewhere(url) ? elsewhereMessage : state.error),
                chinese
              )}
            </p>
            <ButtonBinding7
              setError={setError}
              command={commandCallback}
              chinese={chinese}
            />
          </div>
        ) : state.loading ? (
          <div
            role="status"
            className="flex h-full items-center justify-center gap-2 text-body text-muted-foreground"
          >
            <Loader2 className="size-4 animate-spin" />
            {state.phase === "connecting_device"
              ? chinese
                ? "正在连接设备…"
                : "Connecting to device…"
              : state.phase === "connecting_service"
                ? chinese
                  ? "正在连接服务…"
                  : "Connecting to service…"
                : chinese
                  ? "正在打开网页…"
                  : "Opening webpage…"}
          </div>
        ) : null}
      </div>
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-1 border-t p-2">
        <Button variant="ghost" size="sm" onClick={onClickProp8}>
          <Globe className="size-4" />
          {chinese ? "引用到主对话" : "Reference in main chat"}
        </Button>
        <ChromeProfileImport
          key={JSON.stringify([scope.userId, scope.workspaceId, id])}
          scope={scope}
          previewId={id}
          chinese={chinese}
        />
        <Button
          disabled={
            device.id !== "local" && isServicePreviewUrl(state.url || url)
          }
          variant="ghost"
          size="sm"
          onClick={onClickProp9}
        >
          <ExternalLink className="size-4" />
          {chinese ? "在浏览器中打开" : "Open in browser"}
        </Button>
      </div>
    </div>
  );
}

const ButtonBinding1 = observer(function ButtonBinding1({
  query,
  chinese,
  ...slotProps
}: {
  query:
    | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
        LocalPreviewLocation[],
        Error
      >
    | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
        LocalPreviewLocation[],
        Error
      >;
  chinese: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    query:
      | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
          LocalPreviewLocation[],
          Error
        >
      | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
          LocalPreviewLocation[],
          Error
        >;
    chinese: boolean;
  }
>) {
  const onClickProp10 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void query.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button variant="ghost" size="xs" onClick={onClickProp10}>
        {chinese ? "重试" : "Retry"}
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding2 = observer(function ButtonBinding2({
  entry,
  selected,
  onSelect,
  ...slotProps
}: {
  entry: LocalPreviewLocation;
  selected: string | undefined;
  onSelect: (file: LocalPreviewLocation) => void;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    entry: LocalPreviewLocation;
    selected: string | undefined;
    onSelect: (file: LocalPreviewLocation) => void;
  }
>) {
  const onClickProp11 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => onSelect(entry));
  return (
    <Slot.Root {...slotProps}>
      <Button
        key={entry.path}
        variant="ghost"
        size="sm"
        className={`w-full justify-start ${selected === entry.path ? "bg-surface-selected hover:bg-surface-selected" : ""}`}
        title={entry.name}
        onClick={onClickProp11}
      >
        <File className="size-4 shrink-0" />
        <span className="truncate">{entry.name}</span>
      </Button>
    </Slot.Root>
  );
});

const ButtonBinding3 = observer(function ButtonBinding3({
  setMode,
  previewKey,
  source,
  chinese,
  ...slotProps
}: {
  setMode: import("react").Dispatch<
    import("react").SetStateAction<{ key: string; source: boolean }>
  >;
  previewKey: string;
  source: boolean;
  chinese: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setMode: import("react").Dispatch<
      import("react").SetStateAction<{ key: string; source: boolean }>
    >;
    previewKey: string;
    source: boolean;
    chinese: boolean;
  }
>) {
  const onClickProp12 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => setMode({ key: previewKey, source: !source }));
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="ghost"
        size="xs"
        className="shrink-0"
        onClick={onClickProp12}
      >
        {source ? <Eye className="size-3.5" /> : <Code2 className="size-3.5" />}
        {source ? (chinese ? "预览" : "Preview") : chinese ? "源码" : "Source"}
      </Button>
    </Slot.Root>
  );
});

const IconButtonBinding4 = observer(function IconButtonBinding4({
  chinese,
  file
}: {
  chinese: boolean;
  file: import("../../../../../../packages/core/local-conversation/protocol").LocalPreviewFile;
}) {
  const onClickProp13 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof IconButton>["onClick"]>
  >(() => {
    void copyText(file.text!);
  });
  return (
    <IconButton
      label={chinese ? "复制文件内容" : "Copy file contents"}
      onClick={onClickProp13}
    >
      <Copy />
    </IconButton>
  );
});

const ButtonBinding5 = observer(function ButtonBinding5({
  query,
  chinese,
  ...slotProps
}: {
  query:
    | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
        import("../../../../../../packages/core/local-conversation/protocol").LocalPreviewFile,
        Error
      >
    | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
        import("../../../../../../packages/core/local-conversation/protocol").LocalPreviewFile,
        Error
      >;
  chinese: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    query:
      | import("@tanstack/react-query").QueryObserverRefetchErrorResult<
          import("../../../../../../packages/core/local-conversation/protocol").LocalPreviewFile,
          Error
        >
      | import("@tanstack/react-query").QueryObserverLoadingErrorResult<
          import("../../../../../../packages/core/local-conversation/protocol").LocalPreviewFile,
          Error
        >;
    chinese: boolean;
  }
>) {
  const onClickProp14 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    void query.refetch();
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        variant="outline"
        size="sm"
        className="mt-3"
        onClick={onClickProp14}
      >
        {chinese ? "重试" : "Retry"}
      </Button>
    </Slot.Root>
  );
});

const MarkdownBinding6 = observer(function MarkdownBinding6({
  previewLines
}: {
  previewLines: string[];
}) {
  const onUrlClickProp15 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Markdown>["onUrlClick"]>
  >((url) => {
    if (/^(https?:|mailto:)/i.test(url))
      void window.desktopAPI.openExternal(url);
  });
  return (
    <Markdown mode="full" onUrlClick={onUrlClickProp15}>
      {previewLines.join("\n")}
    </Markdown>
  );
});

const ButtonBinding7 = observer(function ButtonBinding7({
  setError,
  command,
  chinese,
  ...slotProps
}: {
  setError: import("react").Dispatch<import("react").SetStateAction<string>>;
  command: (value: Omit<WebPreviewCommand, "id">) => Promise<void>;
  chinese: boolean;
} & Omit<
  React.ComponentProps<typeof Slot.Root>,
  keyof {
    setError: import("react").Dispatch<import("react").SetStateAction<string>>;
    command: (value: Omit<WebPreviewCommand, "id">) => Promise<void>;
    chinese: boolean;
  }
>) {
  const onClickProp16 = usePersistCallback<
    NonNullable<React.ComponentProps<typeof Button>["onClick"]>
  >(() => {
    setError("");
    void command({ action: "reload" });
  });
  return (
    <Slot.Root {...slotProps}>
      <Button
        className="mt-3"
        variant="outline"
        size="sm"
        onClick={onClickProp16}
      >
        {chinese ? "重试" : "Retry"}
      </Button>
    </Slot.Root>
  );
});
