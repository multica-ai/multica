import { ApiError } from "@multica/core/api";
import { AlertTriangle } from "@multica/ui/icons";
import { Button } from "@multica/ui/components/ui/button";
import { useT } from "../../i18n";

export const GRAPH_ERROR_POLICY = {
  403: { hideGraph: true, retry: false, messageKey: "error_forbidden" },
  404: { hideGraph: true, retry: false, messageKey: "error_unavailable" },
  405: { hideGraph: true, retry: false, messageKey: "error_unavailable" },
  422: { hideGraph: true, retry: true, messageKey: "error_unverified" },
  504: { hideGraph: false, retry: true, messageKey: "error_timeout" },
} as const;
export function graphErrorPolicy(error: Error | null) {
  const status = error instanceof ApiError ? error.status : 0;
  return (
    GRAPH_ERROR_POLICY[status as keyof typeof GRAPH_ERROR_POLICY] ?? {
      hideGraph: false,
      retry: true,
      messageKey: null,
    }
  );
}

export function DagErrorState({
  error,
  onRetry,
}: {
  error: Error | null;
  onRetry: () => void;
}) {
  const { t } = useT("dag");
  const { messageKey, retry } = graphErrorPolicy(error);
  return (
    <div
      className="flex flex-1 min-h-0 flex-col items-center justify-center gap-3 text-muted-foreground"
      role="alert"
    >
      <AlertTriangle className="h-10 w-10 text-faint-foreground" />
      <p className="text-body">{t(($) => $.error_title)}</p>
      <p className="text-caption">
        {messageKey
          ? t(($) => $[messageKey])
          : (error?.message ?? t(($) => $.error_title))}
      </p>
      {retry && (
        <Button variant="outline" size="sm" className="mt-1" onClick={onRetry}>
          {t(($) => $.error_retry)}
        </Button>
      )}
    </div>
  );
}
