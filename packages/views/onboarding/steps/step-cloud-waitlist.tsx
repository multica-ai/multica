"use client";

import { useState } from "react";
import { ArrowLeft, Cloud } from "lucide-react";
import { Button } from "@multica/ui/components/ui/button";
import { Input } from "@multica/ui/components/ui/input";

export function StepCloudWaitlist({
  onBack,
  onSubmit,
}: {
  onBack: () => void;
  onSubmit: (email: string) => void;
}) {
  const [email, setEmail] = useState("");
  const valid = /^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email);

  return (
    <div className="flex w-full flex-col gap-6">
      <button
        type="button"
        onClick={onBack}
        className="flex items-center gap-1.5 self-start text-sm text-muted-foreground hover:text-foreground"
      >
        <ArrowLeft className="h-3.5 w-3.5" />
        Back to options
      </button>

      <div className="flex flex-col items-center gap-3 text-center">
        <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-muted">
          <Cloud className="h-5 w-5 text-muted-foreground" />
        </div>
        <h1 className="text-2xl font-semibold tracking-tight">
          Join the cloud waitlist
        </h1>
        <p className="text-sm text-muted-foreground">
          Cloud runtimes are coming. We&apos;ll email you when they&apos;re ready.
        </p>
      </div>

      <form
        onSubmit={(e) => {
          e.preventDefault();
          if (!valid) return;
          onSubmit(email);
        }}
        className="flex flex-col gap-3"
      >
        <Input
          type="email"
          placeholder="you@example.com"
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          autoFocus
        />
        <Button type="submit" size="lg" disabled={!valid}>
          Join waitlist
        </Button>
      </form>
    </div>
  );
}
