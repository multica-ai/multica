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
  "Create and work with issues (tasks) — describe, comment, attach files, and follow them through to resolution.",
  "Collaborate with AI agents — assign them tasks, ask questions, and have them solve things for you.",
  "Use inbox, channels, and chat to stay updated and communicate with both colleagues and agents.",
  "View dashboards, documents, and the artifacts agents produce along the way.",
  "Install Multica as a desktop app and as an app on your phone (see the steps below).",
];

/** What a member cannot do — those actions are reserved for admins. */
const MEMBER_CANNOT = [
  "Change workspace settings or billing.",
  "Create, edit, or delete agents at the workspace level — that is managed by an admin.",
  "Manage other members' access, roles, or invitations.",
  "Change tool policies and security settings for the agents.",
  "Delete the workspace or other people's data.",
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
          Log out
        </Button>
      )}

      <div className="mx-auto w-full max-w-3xl px-6 py-12 sm:py-16">
        {/* Hero */}
        <header className="flex flex-col items-center gap-4 text-center">
          <div className="flex h-14 w-14 items-center justify-center rounded-2xl bg-primary/10">
            <Sparkles className="h-7 w-7 text-primary" />
          </div>
          <h1 className="text-3xl font-semibold tracking-tight sm:text-4xl">
            Welcome to Firtal Multica
          </h1>
          <p className="max-w-xl text-muted-foreground">
            Firtal's AI platform. Here you work with agents, issues, and chats —
            all in one place. Start by reading what you can do as a member, and
            then install the app if you want.
          </p>
        </header>

        {/* 1. Member documentation */}
        <section className="mt-12">
          <SectionHeader
            icon={<BookOpen className="h-5 w-5" />}
            title="1. What can you do as a member?"
            description="Read this first. It gives you an overview of what you can do — and what only an admin can do — in Firtal Multica."
          />
          <Card className="mt-4 p-6">
            <div className="grid gap-6 sm:grid-cols-2">
              <div>
                <h3 className="flex items-center gap-2 text-sm font-semibold text-emerald-600 dark:text-emerald-400">
                  <Check className="h-4 w-4" />
                  What you can do
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
                  Admin only
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
            title="2. Download the desktop app (recommended)"
            description="It gives you the best experience: faster shortcuts, native notifications, and multiple work panes side by side. You can also just stay in the browser."
          />
          <Card className="mt-4 p-6">
            <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
              <div className="text-sm text-muted-foreground">
                Download the app for Mac or Windows. On Mac, drag Multica to
                the Applications folder; on Windows, follow the installation guide.
                The first time you log in, use the same account as here.
              </div>
              <a
                className={`${buttonVariants()} shrink-0`}
                href={DESKTOP_DOWNLOAD_URL}
                target="_blank"
                rel="noopener noreferrer"
              >
                <Download className="h-4 w-4" />
                Download desktop app
              </a>
            </div>
          </Card>
        </section>

        {/* 3. PWA */}
        <section className="mt-10">
          <SectionHeader
            icon={<Smartphone className="h-5 w-5" />}
            title="3. Install Multica as an app on your phone"
            description="You can add Multica as an app (PWA) on both iPhone and Android — it will work just like a native app."
          />
          <Card className="mt-4 p-2 sm:p-4">
            <Accordion className="w-full">
              <AccordionItem value="ios">
                <AccordionTrigger className="px-4">
                  iOS (Safari)
                </AccordionTrigger>
                <AccordionContent className="px-4">
                  <ol className="ml-5 list-decimal space-y-2 text-sm text-muted-foreground">
                    <li>Open Multica in Safari on your iPhone (not Chrome).</li>
                    <li>
                      Tap the <strong>Share</strong> button at the bottom (the square with
                      the arrow).
                    </li>
                    <li>
                      Select <strong>Add to Home Screen</strong> in the menu.
                    </li>
                    <li>
                      Give it a name (Multica) and tap <strong>Add</strong>.
                    </li>
                    <li>
                      Open the app from the home screen — it now runs full screen,
                      without the Safari frame.
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
                    <li>Open Multica in Chrome on your Android.</li>
                    <li>
                      Tap the menu (the three dots) in the top right.
                    </li>
                    <li>
                      Select <strong>Install app</strong> or{" "}
                      <strong>Add to home screen</strong>.
                    </li>
                    <li>Confirm in the dialog.</li>
                    <li>
                      Open Multica from the app drawer — push notifications work
                      just like in a regular app.
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
            title="4. How to get help and report issues"
            description="All support happens inside Multica Support — not here."
          />
          <Card className="mt-4 p-6">
            <p className="text-sm text-muted-foreground">
              If you encounter an error or need help, everything happens
              inside Multica Support. You open a case there yourself, and Knud —
              a friendly and knowledgeable person — takes it from there and helps you further.
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
                    I have read and understood that all support and bug reporting
                    happens inside Multica Support, where Knud helps.
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
              Confirm the support step above before continuing.
            </p>
          )}
          <Button
            size="lg"
            onClick={handleComplete}
            disabled={!supportAcknowledged}
          >
            Go to my inbox
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
