import { AlertTriangle, ArrowLeft, Home } from "lucide-react";
import {
  isRouteErrorResponse,
  useLocation,
  useNavigate,
  useParams,
  useRouteError,
} from "react-router-dom";
import { Button } from "@multica/ui/components/ui/button";

function getErrorDetails(error: unknown) {
  if (isRouteErrorResponse(error)) {
    return {
      status: error.status,
      title: error.status === 404 ? "Page not found" : "Something went wrong",
      message:
        error.status === 404
          ? "This page does not exist, or it may have moved."
          : error.statusText || "Multica hit an unexpected error while opening this page.",
    };
  }

  if (error instanceof Error) {
    return {
      status: undefined,
      title: "Something went wrong",
      message: error.message || "Multica hit an unexpected error while opening this page.",
    };
  }

  return {
    status: undefined,
    title: "Something went wrong",
    message: "Multica hit an unexpected error while opening this page.",
  };
}

export function RouteErrorBoundary() {
  const error = useRouteError();
  return <RouteErrorState {...getErrorDetails(error)} />;
}

export function NotFoundPage() {
  return (
    <RouteErrorState
      status={404}
      title="Page not found"
      message="This page does not exist, or it may have moved."
    />
  );
}

function RouteErrorState({
  status,
  title,
  message,
}: {
  status?: number;
  title: string;
  message: string;
}) {
  const navigate = useNavigate();
  const location = useLocation();
  const { workspaceSlug } = useParams<{ workspaceSlug?: string }>();
  const homePath = workspaceSlug ? `/${workspaceSlug}/issues` : "/";

  return (
    <main className="flex min-h-full items-center justify-center bg-background px-6 py-16 text-foreground">
      <section className="w-full max-w-md rounded-2xl border border-border bg-card p-8 text-center shadow-sm">
        <div className="mx-auto mb-5 flex size-12 items-center justify-center rounded-full bg-muted text-muted-foreground">
          <AlertTriangle className="size-6" aria-hidden="true" />
        </div>

        {status ? (
          <p className="mb-2 text-sm font-medium text-muted-foreground">Error {status}</p>
        ) : null}
        <h1 className="text-2xl font-semibold tracking-tight">{title}</h1>
        <p className="mt-3 text-sm leading-6 text-muted-foreground">{message}</p>

        {location.pathname ? (
          <p className="mt-4 truncate rounded-lg bg-muted px-3 py-2 text-xs text-muted-foreground">
            {location.pathname}
          </p>
        ) : null}

        <div className="mt-6 flex flex-col-reverse gap-3 sm:flex-row sm:justify-center">
          <Button type="button" variant="outline" onClick={() => navigate(-1)}>
            <ArrowLeft className="size-4" aria-hidden="true" />
            Go back
          </Button>
          <Button type="button" onClick={() => navigate(homePath, { replace: true })}>
            <Home className="size-4" aria-hidden="true" />
            Go to issues
          </Button>
        </div>
      </section>
    </main>
  );
}
