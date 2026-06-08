"use client";

import { Component, type ReactNode } from "react";
import { RotateCcw } from "lucide-react";

interface Props {
  children: ReactNode;
  // When provided, render this on error instead of the default reset panel.
  // Pass `null` to fail silently (used for the portaled modal host).
  fallback?: ReactNode;
}
interface State {
  hasError: boolean;
}

/**
 * Isolates the live product demo. If any interaction throws during render,
 * we show a small reset panel instead of letting the error bubble up and
 * white-screen the whole landing page. "Reset" remounts the demo subtree.
 */
export class DemoErrorBoundary extends Component<Props, State> {
  state: State = { hasError: false };

  static getDerivedStateFromError(): State {
    return { hasError: true };
  }

  componentDidCatch() {
    // Swallow — the demo is non-critical marketing UI. Nothing to report.
  }

  private reset = () => this.setState({ hasError: false });

  render() {
    if (this.state.hasError) {
      if ("fallback" in this.props) return this.props.fallback ?? null;
      return (
        <div className="flex h-full w-full flex-col items-center justify-center gap-3 bg-white text-center">
          <p className="text-[14px] text-[#0a0d12]/55">
            The demo hit a snag.
          </p>
          <button
            type="button"
            onClick={this.reset}
            className="inline-flex items-center gap-1.5 rounded-[8px] border border-[#0a0d12]/14 bg-white px-3.5 py-2 text-[13px] font-semibold text-[#0a0d12] transition-colors hover:bg-[#0a0d12]/[0.04]"
          >
            <RotateCcw className="size-3.5" aria-hidden />
            Reset demo
          </button>
        </div>
      );
    }
    return this.props.children;
  }
}
