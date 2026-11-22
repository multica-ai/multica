"use client";

import { createContext, useContext } from "react";
import { toast } from "sonner";
import type { IssueTableQuerySpec } from "@multica/core/types";

export const REVIEW_PARAM = "review";
export interface ReviewOrigin {
  token: string;
  workspaceId: string;
  userId: string;
  listPath: string;
  query: IssueTableQuerySpec;
  sourceKey: string;
  /** A DOM memento only; never an issue-data cache. */
  scroll: Array<{ top: number; left: number }>;
}
export interface ReviewSession {
  owner?: string;
  origin: ReviewOrigin | null;
  epoch: number;
  location: string;
  pendingLocation?: string;
  preserveNavigation?: string;
  navigate: (path?: string) => void;
  busy: boolean;
  receipt: number;
  toastId?: string | number;
  invalidate: () => void;
}
export function createReviewSession(owner?: string): ReviewSession {
  const session: ReviewSession = {
    owner, origin: null, epoch: 0, location: "", busy: false, receipt: 0,
    navigate: (path) => {
      session.epoch++;
      session.pendingLocation = path;
      if (!path || session.preserveNavigation !== path) {
        session.receipt++;
        if (session.toastId !== undefined) toast.dismiss(session.toastId);
      }
      session.preserveNavigation = undefined;
    },
    invalidate: () => {
      session.origin = null;
      session.receipt++;
      if (session.toastId !== undefined) toast.dismiss(session.toastId);
    },
  };
  return session;
}
export const ReviewSessionContext = createContext<ReviewSession | null>(null);
export const ReviewOriginContext = createContext<((href: string) => string) | null>(null);
export const ReviewDetailContext = createContext<string | null>(null);

export function useReviewOriginLink() { return useContext(ReviewOriginContext); }

export function reviewHref(path: string, token: string) {
  const url = new URL(path, "https://local.invalid");
  url.searchParams.set(REVIEW_PARAM, token);
  return url.pathname + url.search + url.hash;
}
