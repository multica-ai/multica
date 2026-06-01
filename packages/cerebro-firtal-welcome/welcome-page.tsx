"use client";

import { useState } from "react";
import {
  ArrowRight,
  BookOpen,
  Check,
  Download,
  LifeBuoy,
  LogOut,
  Monitor,
  Smartphone,
  Sparkles,
  X,
} from "lucide-react";
import { Button, buttonVariants } from "@multica/ui/components/ui/button";
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from "@multica/ui/components/ui/accordion";
import { Card } from "@multica/ui/components/ui/card";
import { Label } from "@multica/ui/components/ui/label";
import {
  RadioGroup,
  RadioGroupItem,
} from "@multica/ui/components/ui/radio-group";
import { markFirtalWelcomeSeen } from "./welcome-state";

const DESKTOP_DOWNLOAD_URL = "/download";

/** What a member can do in Firtal Multica. */
const MEMBER_CAN = [
  "Oprette og arbejde med issues (opgaver) — beskrive, kommentere, vedhæfte filer og følge dem til de er løst.",
  "Samarbejde med AI-agenterne — tildele dem opgaver, stille spørgsmål og få dem til at løse ting for dig.",
  "Bruge inbox, kanaler og chat til at følge med og kommunikere med både kolleger og agenter.",
  "Se dashboards, dokumenter og de artefakter agenterne producerer undervejs.",
  "Installere Multica som desktop-app og som app på din telefon (se trinnene længere nede).",
];

/** What a member cannot do — those actions are reserved for admins. */
const MEMBER_CANNOT = [
  "Ændre workspace-indstillinger eller fakturering.",
  "Oprette, redigere eller slette agenter på workspace-niveau — det styrer en admin.",
  "Administrere andre medlemmers adgang, roller eller invitationer.",
  "Ændre tool-policies og sikkerhedsindstillinger for agenterne.",
  "Slette workspacet eller andres data.",
];

export interface FirtalWelcomePageProps {
  /** Current user id — used to mark the welcome as seen in localStorage. */
  userId: string;
  /** Optional logout handler; renders a Log out button in the top-right when provided. */
  onLogout?: () => void;
  /**
   * Called when the user leaves the welcome page (typically routed to the
   * inbox by the caller). No desktop gate anymore — the only requirement
   * before continuing is acknowledging how support works.
   */
  onComplete: () => void;
}

/**
 * Firtal-branded welcome page shown to new members of cerebro-fork
 * workspaces (gated by `cerebro_firtal_welcome` feature flag).
 *
 * Structure (FIR-2490, revised per Jesper 2026-06-01):
 *   1. Member documentation — what you can and cannot do as a member.
 *      This is the most important step, so it comes first. Rendered as
 *      plain inline content so the page scrolls normally (no nested
 *      fixed-height container that traps the scroll).
 *   2. Desktop app — optional recommendation, no hard gate.
 *   3. PWA install guides — iOS Safari + Android Chrome accordion.
 *   4. Support — all support happens inside Multica Support, where Knud
 *      helps out. No link; the user ticks a radio to confirm they read it.
 *   5. Done CTA — enabled once support is acknowledged. The app never
 *      blocks on desktop.
 */
export function FirtalWelcomePage({
  userId,
  onLogout,
  onComplete,
}: FirtalWelcomePageProps) {
  const [supportAcknowledged, setSupportAcknowledged] = useState(false);

  const handleComplete = () => {
    markFirtalWelcomeSeen(userId);
    onComplete();
  };

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
            Firtals AI-platform. Her arbejder du med agenter, issues og chats —
            alt samlet ét sted. Start med at læse hvad du kan som member, og
            installér så appen hvis du vil.
          </p>
        </header>

        {/* 1. Member documentation */}
        <section className="mt-12">
          <SectionHeader
            icon={<BookOpen className="h-5 w-5" />}
            title="1. Hvad kan du som member?"
            description="Læs det her først. Det giver dig overblikket over hvad du kan — og hvad kun en admin kan — i Firtal Multica."
          />
          <Card className="mt-4 p-6">
            <div className="grid gap-6 sm:grid-cols-2">
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-emerald-600 dark:text-emerald-400">
                  <Check className="h-4 w-4" />
                  Det kan du
                </h3>
                <ul className="mt-3 space-y-2">
                  {MEMBER_CAN.map((item) => (
                    <li
                      key={item}
                      className="flex gap-2 text-sm text-muted-foreground"
                    >
                      <Check className="mt-0.5 h-4 w-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
                      <span>{item}</span>
                    </li>
                  ))}
                </ul>
              </div>
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-muted-foreground">
                  <X className="h-4 w-4" />
                  Det kan kun en admin
                </h3>
                <ul className="mt-3 space-y-2">
                  {MEMBER_CANNOT.map((item) => (
                    <li
                      key={item}
                      className="flex gap-2 text-sm text-muted-foreground"
                    >
                      <X className="mt-0.5 h-4 w-4 shrink-0 text-muted-foreground/70" />
                      <span>{item}</span>
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </Card>
        </section>

        {/* 2. Desktop app — optional, no gate */}
        <section className="mt-10">
          <SectionHeader
            icon={<Monitor className="h-5 w-5" />}
            title="2. Hent desktop-appen (anbefalet)"
            description="Den giver dig den bedste oplevelse: hurtigere genveje, native notifikationer og flere arbejdsruder side om side. Du kan også bare blive i browseren."
          />
          <Card className="mt-4 p-6">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm text-muted-foreground">
                Hent appen til Mac eller Windows. På Mac trækker du Multica til
                Programmer-mappen; på Windows følger du installationsguiden.
                Første gang logger du ind med samme konto som her.
              </div>
              <a
                className={`${buttonVariants()} shrink-0`}
                href={DESKTOP_DOWNLOAD_URL}
                target="_blank"
                rel="noopener noreferrer"
              >
                <Download className="h-4 w-4" />
                Download desktop-app
              </a>
            </div>
          </Card>
        </section>

        {/* 3. PWA */}
        <section className="mt-10">
          <SectionHeader
            icon={<Smartphone className="h-5 w-5" />}
            title="3. Installér Multica som app på din telefon"
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

        {/* 4. Support */}
        <section className="mt-10">
          <SectionHeader
            icon={<LifeBuoy className="h-5 w-5" />}
            title="4. Sådan får du hjælp og melder fejl"
            description="Al support foregår inde i Multica Support — ikke her."
          />
          <Card className="mt-4 p-6">
            <p className="text-sm text-muted-foreground">
              Hvis du støder på en fejl eller har brug for hjælp, sker det hele
              inde i Multica Support. Du opretter selv en sag derinde, og Knud —
              en flink og vidende fyr — tager den derfra og hjælper dig videre.
            </p>
            <div className="mt-5 rounded-lg border border-border bg-muted/40 p-4">
              <RadioGroup
                value={supportAcknowledged ? "ack" : ""}
                onValueChange={(value) =>
                  setSupportAcknowledged(value === "ack")
                }
              >
                <div className="flex items-start gap-3">
                  <RadioGroupItem
                    value="ack"
                    id="support-ack"
                    className="mt-0.5"
                  />
                  <Label
                    htmlFor="support-ack"
                    className="text-sm font-normal leading-relaxed text-foreground"
                  >
                    Jeg har læst og forstået, at al support og fejlmelding
                    foregår inde i Multica Support, hvor Knud hjælper.
                  </Label>
                </div>
              </RadioGroup>
            </div>
          </Card>
        </section>

        {/* Done CTA */}
        <div className="mt-12 flex flex-col items-center gap-3">
          {!supportAcknowledged && (
            <p className="text-sm text-muted-foreground">
              Bekræft support-trinnet herover, før du går videre.
            </p>
          )}
          <Button
            size="lg"
            onClick={handleComplete}
            disabled={!supportAcknowledged}
          >
            Gå til min indbakke
            <ArrowRight className="h-4 w-4" />
          </Button>
        </div>
      </div>
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
