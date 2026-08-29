import { ShieldCheck, Terminal } from "lucide-react";
import {
  REPLACEMENT_RUNTIME_PROVIDERS,
  type RuntimeProviderDescriptor,
} from "@multica/core/runtimes";
import { useT } from "../../i18n";
import { ProviderLogo } from "./provider-logo";

type RuntimesT = ReturnType<typeof useT<"runtimes">>["t"];

function setupLabel(
  provider: RuntimeProviderDescriptor,
  t: RuntimesT,
): string {
  switch (provider.setup) {
    case "subscription":
      return t(($) => $.provider_support.setup.subscription);
    case "api_key":
      return t(($) => $.provider_support.setup.api_key);
    case "local":
      return t(($) => $.provider_support.setup.local);
  }
}

function providerEndpoint(
  provider: RuntimeProviderDescriptor,
  t: RuntimesT,
): string {
  if (provider.setup === "subscription") {
    return t(($) => $.provider_support.endpoint.sign_in);
  }
  return (
    provider.defaultBaseUrl ?? t(($) => $.provider_support.endpoint.daemon_configured)
  );
}

function ProviderSupportRow({
  provider,
}: {
  provider: RuntimeProviderDescriptor;
}) {
  const { t } = useT("runtimes");
  const isAPI = provider.execution === "openai-compatible";
  return (
    <div className="flex min-w-0 items-start gap-3 rounded-lg border bg-card px-3 py-3">
      <div className="mt-0.5 flex h-8 w-8 shrink-0 items-center justify-center rounded-md border bg-muted/40">
        <ProviderLogo provider={provider.id} className="h-5 w-5" />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1">
          <span className="truncate text-body-sm font-medium">
            {provider.displayName}
          </span>
          <span className="rounded-full bg-muted px-1.5 py-0.5 text-micro text-muted-foreground">
            {setupLabel(provider, t)}
          </span>
        </div>
        <p className="mt-1 truncate font-mono text-micro text-muted-foreground">
          {isAPI ? provider.apiKeyEnv : providerEndpoint(provider, t)}
        </p>
        {isAPI && provider.defaultBaseUrl && (
          <p className="mt-0.5 truncate text-micro text-muted-foreground">
            {providerEndpoint(provider, t)}
          </p>
        )}
      </div>
    </div>
  );
}

export function ProviderSupportCard() {
  const { t } = useT("runtimes");
  return (
    <section
      aria-labelledby="provider-support-title"
      className="mb-6 overflow-hidden rounded-xl border bg-card"
    >
      <div className="border-b px-4 py-4 sm:px-5">
        <div className="flex items-start gap-3">
          <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Terminal className="h-5 w-5" aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <h2 id="provider-support-title" className="text-body font-semibold">
              {t(($) => $.provider_support.title)}
            </h2>
            <p className="mt-1 max-w-3xl text-caption leading-relaxed text-muted-foreground">
              {t(($) => $.provider_support.description)}
            </p>
          </div>
        </div>
      </div>

      <div className="grid gap-2 p-4 sm:grid-cols-2 sm:p-5 lg:grid-cols-3">
        {REPLACEMENT_RUNTIME_PROVIDERS.map((provider) => (
          <ProviderSupportRow key={provider.id} provider={provider} />
        ))}
      </div>

      <div className="flex items-start gap-3 border-t bg-muted/20 px-4 py-3 text-caption text-muted-foreground sm:px-5">
        <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-success" aria-hidden="true" />
        <p>
          {t(($) => $.provider_support.security_note)}
        </p>
      </div>
    </section>
  );
}
