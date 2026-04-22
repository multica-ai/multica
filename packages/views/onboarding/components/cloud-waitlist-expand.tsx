"use client";

import { useState } from "react";
import { ArrowRight, Check, Loader2 } from "lucide-react";
import { toast } from "sonner";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";
import { Label } from "@multica/ui/components/ui/label";
import { Textarea } from "@multica/ui/components/ui/textarea";
import { joinCloudWaitlist } from "@multica/core/onboarding";

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const REASON_MAX = 500;

/**
 * Cloud waitlist inline form — used from both:
 *   - web Step 3 (`StepPlatformFork` cloud fork)
 *   - desktop Step 3 empty state (`StepRuntimeConnect`)
 *
 * Submitting calls `joinCloudWaitlist` and disables the form. Does NOT
 * advance the onboarding flow — the caller owns navigation (usually
 * "Skip for now" in the footer). That keeps the contract consistent:
 * waitlist is interest capture, Skip is the actual exit.
 */
export function CloudWaitlistExpand({
  submitted,
  onSubmitted,
}: {
  submitted: boolean;
  onSubmitted: () => void;
}) {
  const [email, setEmail] = useState("");
  const [reason, setReason] = useState("");
  const [submitting, setSubmitting] = useState(false);

  const canSubmit =
    !submitted &&
    !submitting &&
    EMAIL_PATTERN.test(email.trim()) &&
    reason.trim().length <= REASON_MAX;

  const submit = async () => {
    if (!canSubmit) return;
    setSubmitting(true);
    try {
      await joinCloudWaitlist(email.trim(), reason.trim());
      toast.success(
        "You're on the list. We'll email when cloud runtimes are live.",
      );
      onSubmitted();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Failed to join waitlist",
      );
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex flex-col gap-4 rounded-lg border bg-muted/40 p-5">
      <p className="text-[13.5px] leading-[1.55] text-foreground/85">
        Cloud runtimes aren&apos;t live yet. Leave your email and we&apos;ll
        reach out when they are.{" "}
        <span className="text-foreground/70">
          Heads-up: agents can&apos;t execute tasks without a runtime — if
          you hit Skip now, your workspace is read-only until you come back
          and install one.
        </span>
      </p>

      <div className="flex flex-col gap-1.5">
        <Label
          htmlFor="waitlist-email"
          className="text-xs font-medium text-muted-foreground"
        >
          Email
        </Label>
        <Input
          id="waitlist-email"
          type="email"
          autoComplete="email"
          value={email}
          disabled={submitted}
          placeholder="you@work.com"
          onChange={(e) => setEmail(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              submit();
            }
          }}
        />
      </div>

      <div className="flex flex-col gap-1.5">
        <Label
          htmlFor="waitlist-reason"
          className="text-xs font-medium text-muted-foreground"
        >
          Why cloud?
          <span className="ml-2 font-normal text-muted-foreground/70">
            Optional
          </span>
        </Label>
        <Textarea
          id="waitlist-reason"
          value={reason}
          disabled={submitted}
          onChange={(e) => setReason(e.target.value)}
          placeholder="e.g. we want agents running 24/7, or my team works across different devices."
          rows={3}
          maxLength={REASON_MAX}
        />
      </div>

      <div className="flex items-center justify-end">
        <Button size="lg" disabled={submitted || !canSubmit} onClick={submit}>
          {submitting && <Loader2 className="h-4 w-4 animate-spin" />}
          {submitted ? (
            <>
              <Check className="h-4 w-4" />
              You&apos;re on the list
            </>
          ) : (
            <>
              Join waitlist
              <ArrowRight className="h-4 w-4" />
            </>
          )}
        </Button>
      </div>
    </div>
  );
}
