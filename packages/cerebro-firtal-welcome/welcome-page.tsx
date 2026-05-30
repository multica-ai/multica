"use client";

import { useState } from "react";
import {
  ArrowRight,
  Bug,
  CheckCircle2,
  Download,
  ExternalLink,
  LogOut,
  Monitor,
  Shield,
  Smartphone,
  Sparkles,
} from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@multica/ui/components/ui/accordion";
import { Card } from "@multica/ui/components/ui/card";
import { markFirtalWelcomeSeen } from "./welcome-state";

const SUPPORT_URL = "https://multica.firtal.com";
const DESKTOP_DOWNLOAD_URL = "/download";
const MEMBERS_DOCS_URL = "/docs/cerebro";

export interface FirtalWelcomePageProps {
  /** Current user id — used to mark the welcome as seen in localStorage. */
  userId: string;
  /** Optional logout handler; renders a Log out button in the top-right when provided. */
  onLogout?: () => void;
  /**
   * Called when the user is ready to leave the welcome page. Receives a
   * boolean indicating whether the user installed the desktop app or
   * explicitly skipped it. The caller decides where to send them next
   * (typically the inbox).
   */
  onComplete: (opts: { skippedDesktop: boolean }) => void;
}

/**
 * Firtal-branded welcome page shown to new members of cerebro-fork
 * workspaces (gated by `cerebro_firtal_welcome` feature flag).
 *
 * Structure:
 *   1. Hero — Firtal Multica branding, one-line description, primary CTA.
 *   2. Desktop install guide — hard-gate modal on download, escape via
 *      "Jeg vil ikke have desktop-appen".
 *   3. PWA install guides — iOS Safari + Android Chrome accordion.
 *   4. Members documentation link.
 *   5. Bug reporting — opens the Multica Support workspace in a new tab.
 *   6. Done CTA — marks the welcome seen and calls onComplete.
 */
export function FirtalWelcomePage({
  userId,
  onLogout,
  onComplete,
}: FirtalWelcomePageProps) {
  const [desktopModalOpen, setDesktopModalOpen] = useState(false);
  const [desktopChoice, setDesktopChoice] = useState<"installed" | "skipped" | null>(null);

  const handleDownloadClick = () => {
    setDesktopModalOpen(true);
  };

  const handleConfirmDownload = () => {
    setDesktopChoice("installed");
    setDesktopModalOpen(false);
    window.open(DESKTOP_DOWNLOAD_URL, "_blank", "noopener,noreferrer");
  };

  const handleSkipDesktop = () => {
    setDesktopChoice("skipped");
    setDesktopModalOpen(false);
  };

  const handleComplete = () => {
    markFirtalWelcomeSeen(userId);
    onComplete({ skippedDesktop: desktopChoice === "skipped" });
  };

  const desktopGateCleared = desktopChoice !== null;

  return (
    <div className="relative flex min-h-svh flex-col bg-background">
      {onLogout && (
        <Button
          variant="ghost"
          size="sm"
          className="absolute top-4 right-4 sm:top-6 sm:right-8 text-muted-foreground hover:text-destructive"
          onClick={onLogout}
        >
          <LogOut />
          Log ud
        </Button>
      )}

      <div className="mx-auto w-full max-w-3xl px-6 py-12 sm:py-16">
        {/* Hero */}
        <header className="flex flex-col items-center gap-4 text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
            <Sparkles className="h-7 w-7 text-primary" />
          </div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
            Velkommen til Firtal Multica
          </h1>
          <p className="max-w-xl text-muted-foreground">
            Firtals AI-platform. Her arbejder du med agenter, issues og chats — alt
            samlet ét sted. Følg de få trin herunder, så du er klar til at komme i
            gang.
          </p>
        </header>

        {/* 1. Desktop install */}
        <section className="mt-12">
          <SectionHeader
            icon={<Monitor className="h-5 w-5" />}
            title="1. Hent desktop-appen"
            description="Den giver dig den bedste oplevelse: hurtigere genveje, native notifikationer og flere arbejdsruder side om side."
          />
          <Card className="mt-4 p-6">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm text-muted-foreground">
                Klik download, læs den korte installationsvejledning, og bekræft at
                du har forstået trinnene. Det undgår support-bøvl.
              </div>
              <div className="flex shrink-0 items-center gap-3">
                {desktopChoice === "installed" && (
                  <span className="inline-flex items-center gap-1 text-sm text-emerald-600 dark:text-emerald-400">
                    <CheckCircle2 className="h-4 w-4" />
                    Klaret
                  </span>
                )}
                {desktopChoice === "skipped" && (
                  <span className="text-sm text-muted-foreground">
                    Springer over
                  </span>
                )}
                <Button onClick={handleDownloadClick}>
                  <Download className="h-4 w-4" />
                  {desktopChoice ? "Åbn igen" : "Download desktop-app"}
                </Button>
              </div>
            </div>
          </Card>
        </section>

        {/* 2. PWA */}
        <section className="mt-10">
          <SectionHeader
            icon={<Smartphone className="h-5 w-5" />}
            title="2. Installér Multica som app på din telefon"
            description="Du kan tilføje Multica som en app (PWA) på både iPhone og Android — så fungerer den ligesom en native app."
          />
          <Card className="mt-4 p-2 sm:p-4">
            <Accordion className="w-full">
              <AccordionItem value="ios">
                <AccordionTrigger className="px-4">
                  iOS (Safari)
                </AccordionTrigger>
                <AccordionContent className="px-4">
                  <ol className="ml-5 list-decimal space-y-2 text-sm text-muted-foreground">
                    <li>Åbn Multica i Safari på din iPhone (ikke Chrome).</li>
                    <li>
                      Tryk på <strong>Del</strong>-knappen nederst (firkanten med
                      pilen).
                    </li>
                    <li>
                      Vælg <strong>Føj til hjemmeskærm</strong> i menuen.
                    </li>
                    <li>
                      Giv den et navn (Multica) og tryk <strong>Tilføj</strong>.
                    </li>
                    <li>
                      Åbn appen fra hjemmeskærmen — den kører nu i fuld skærm,
                      uden Safari-rammen.
                    </li>
                  </ol>
                </AccordionContent>
              </AccordionItem>
              <AccordionItem value="android">
                <AccordionTrigger className="px-4">
                  Android (Chrome)
                </AccordionTrigger>
                <AccordionContent className="px-4">
                  <ol className="ml-5 list-decimal space-y-2 text-sm text-muted-foreground">
                    <li>Åbn Multica i Chrome på din Android.</li>
                    <li>
                      Tryk på menuen (de tre prikker) øverst til højre.
                    </li>
                    <li>
                      Vælg <strong>Installér app</strong> eller{" "}
                      <strong>Føj til startskærm</strong>.
                    </li>
                    <li>Bekræft i dialogen.</li>
                    <li>
                      Åbn Multica fra appoversigten — push-notifikationer virker
                      som i en almindelig app.
                    </li>
                  </ol>
                </AccordionContent>
              </AccordionItem>
            </Accordion>
          </Card>
        </section>

        {/* 3. Members docs */}
        <section className="mt-10">
          <SectionHeader
            icon={<Shield className="h-5 w-5" />}
            title="3. Hvad kan du som member?"
            description="Læs hurtigt op på hvad du kan — og hvad du ikke kan — i Firtal Multica som member."
          />
          <Card className="mt-4 p-6">
            <a
              className={buttonVariants({ variant: "outline" })}
              href={MEMBERS_DOCS_URL}
              target="_blank"
              rel="noopener noreferrer"
            >
              <ExternalLink className="h-4 w-4" />
              Åbn member-dokumentationen
            </a>
          </Card>
        </section>

        {/* 4. Bug reporting */}
        <section className="mt-10">
          <SectionHeader
            icon={<Bug className="h-5 w-5" />}
            title="4. Stødt på en bug? Sådan melder du den"
            description="Bugs meldes ind i Multica Support-workspacet på multica.firtal.com — så ryger de direkte i kø hos os, der vedligeholder platformen."
          />
          <Card className="mt-4 p-6">
            <a
              className={buttonVariants({ variant: "outline" })}
              href={SUPPORT_URL}
              target="_blank"
              rel="noopener noreferrer"
            >
              <ExternalLink className="h-4 w-4" />
              Åbn Multica Support
            </a>
          </Card>
        </section>

        {/* Done CTA */}
        <div className="mt-12 flex flex-col items-center gap-3">
          {!desktopGateCleared && (
            <p className="text-sm text-muted-foreground">
              Tag stilling til desktop-appen herover, før du går videre.
            </p>
          )}
          <Button
            size="lg"
            onClick={handleComplete}
            disabled={!desktopGateCleared}
          >
            Gå til min indbakke
            <ArrowRight className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* Hard-gate modal for desktop install */}
      <Dialog open={desktopModalOpen} onOpenChange={setDesktopModalOpen}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle>Sådan installerer du desktop-appen</DialogTitle>
            <DialogDescription>
              Læs guiden igennem. Du klikker enten download eller springer over —
              du kan ikke fortsætte til din indbakke, før du har taget stilling.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 text-sm">
            <ol className="ml-5 list-decimal space-y-2">
              <li>
                Klik på{" "}
                <strong>Læst og forstået — download</strong>. Vi åbner
                download-siden i en ny fane.
              </li>
              <li>
                Kør installationsfilen og følg trinnene. På Mac trækker du Multica
                til Programmer-mappen. På Windows klikker du gennem
                installationsguiden.
              </li>
              <li>
                Når du åbner appen første gang, logger du ind med samme e-mail som
                her — så er du klar.
              </li>
              <li>
                Hvis du sidder på en arbejds-pc hvor du ikke må installere
                programmer, så vælg <strong>Jeg vil ikke have desktop-appen</strong>
                . Du kan altid bruge Multica i browseren.
              </li>
            </ol>
            <p className="text-xs text-muted-foreground">
              Vi spørger ikke igen — dit valg gemmes nu.
            </p>
          </div>
          <DialogFooter className="flex flex-col gap-2 sm:flex-row sm:justify-between">
            <Button variant="outline" onClick={handleSkipDesktop}>
              Jeg vil ikke have desktop-appen
            </Button>
            <Button onClick={handleConfirmDownload}>
              <Download className="h-4 w-4" />
              Læst og forstået — download
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

function SectionHeader({
  icon,
  title,
  description,
}: {
  icon: React.ReactNode;
  title: string;
  description: string;
}) {
  return (
    <div className="flex items-start gap-3">
      <div className="mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
        {icon}
      </div>
      <div>
        <h2 className="text-lg font-semibold">{title}</h2>
        <p className="mt-1 text-sm text-muted-foreground">{description}</p>
      </div>
    </div>
  );
}
