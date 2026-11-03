import { z } from "zod";
import type {
  ListWeixinInstallationsResponse,
  WeixinLogin,
} from "../types/weixin";

export const WeixinInstallationSchema = z
  .object({
    id: z.string(),
    workspace_id: z.string().default(""),
    agent_id: z.string().default(""),
    bot_id: z.string().default(""),
    user_id: z.string().default(""),
    installer_user_id: z.string().default(""),
    status: z.string().default("revoked"),
    installed_at: z.string().default(""),
  })
  .loose();

export const ListWeixinInstallationsSchema = z
  .object({
    installations: z.array(WeixinInstallationSchema).default([]),
    configured: z.boolean().default(false),
    install_supported: z.boolean().optional(),
  })
  .loose();

export const EMPTY_WEIXIN_INSTALLATIONS: ListWeixinInstallationsResponse = {
  installations: [],
  configured: false,
};

export const WeixinLoginSchema = z
  .object({
    id: z.string().min(1),
    status: z
      .enum([
        "wait",
        "scaned",
        "need_verifycode",
        "scaned_but_redirect",
        "confirmed",
        "expired",
        "verify_code_blocked",
        "binded_redirect",
        "cancelled",
        "error",
      ])
      .catch("error"),
    qr_code_url: z.string().optional(),
    installation_id: z.string().optional(),
    expires_at: z.string().default(""),
  })
  .loose()
  .refine((login) => login.status !== "confirmed" || !!login.installation_id);

export const INVALID_WEIXIN_LOGIN: WeixinLogin = {
  id: "",
  status: "error",
  expires_at: "",
};

export const isWeixinLoginPending = (status?: string) =>
  status === "wait" ||
  status === "scaned" ||
  status === "need_verifycode" ||
  status === "scaned_but_redirect";
