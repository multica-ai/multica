/**
 * Agent Profile 模块
 *
 * 管理 agent 的身份、人格、记忆等配置
 */

import type { AgentProfile, CreateProfileOptions, ProfileConfig, ProfileManagerOptions } from "./types.js";
import type { ToolsConfig } from "../tools/policy.js";
import { DEFAULT_TEMPLATES } from "./templates.js";
import {
  ensureProfileDir,
  getProfileDir,
  loadProfile,
  profileExists,
  saveProfile,
} from "./storage.js";

export { type AgentProfile, type CreateProfileOptions, type ProfileConfig, type ProfileManagerOptions } from "./types.js";
export { DEFAULT_TEMPLATES } from "./templates.js";
export { getProfileDir, profileExists } from "./storage.js";

/**
 * 创建新的 Agent Profile
 *
 * @param profileId - Profile ID
 * @param options - 创建选项
 * @returns 创建的 AgentProfile
 */
export function createAgentProfile(
  profileId: string,
  options?: CreateProfileOptions,
): AgentProfile {
  const { baseDir, useTemplates = true } = options ?? {};

  // 确保目录存在
  ensureProfileDir(profileId, { baseDir });

  // 创建 profile
  const profile: AgentProfile = {
    id: profileId,
  };

  // 如果使用模板，填充默认内容
  if (useTemplates) {
    profile.soul = DEFAULT_TEMPLATES.soul;
    profile.user = DEFAULT_TEMPLATES.user;
    profile.workspace = DEFAULT_TEMPLATES.workspace;
    profile.memory = DEFAULT_TEMPLATES.memory;

    // 保存到文件
    saveProfile(profile, { baseDir });
  }

  return profile;
}

/**
 * 加载 Agent Profile
 *
 * @param profileId - Profile ID
 * @param options - 加载选项
 * @returns AgentProfile，如果不存在返回 undefined
 */
export function loadAgentProfile(
  profileId: string,
  options?: { baseDir?: string | undefined },
): AgentProfile | undefined {
  if (!profileExists(profileId, options)) {
    return undefined;
  }
  return loadProfile(profileId, options);
}

/**
 * 加载或创建 Agent Profile
 *
 * @param profileId - Profile ID
 * @param options - 选项
 * @returns AgentProfile
 */
export function getOrCreateAgentProfile(
  profileId: string,
  options?: CreateProfileOptions,
): AgentProfile {
  const existing = loadAgentProfile(profileId, options);
  if (existing) {
    return existing;
  }
  return createAgentProfile(profileId, options);
}

/**
 * Profile Manager - 用于管理单个 profile 的类
 */
export class ProfileManager {
  private readonly profileId: string;
  private readonly baseDir: string | undefined;
  private profile: AgentProfile | undefined;

  constructor(options: ProfileManagerOptions) {
    this.profileId = options.profileId;
    this.baseDir = options.baseDir;
  }

  /** 获取 profile，如果未加载则加载 */
  getProfile(): AgentProfile | undefined {
    if (!this.profile) {
      this.profile = loadAgentProfile(this.profileId, { baseDir: this.baseDir });
    }
    return this.profile;
  }

  /** 获取或创建 profile */
  getOrCreateProfile(useTemplates = true): AgentProfile {
    if (!this.profile) {
      this.profile = getOrCreateAgentProfile(this.profileId, {
        baseDir: this.baseDir,
        useTemplates,
      });
    }
    return this.profile;
  }

  /** 获取 profile 目录路径 */
  getProfileDir(): string {
    return getProfileDir(this.profileId, { baseDir: this.baseDir });
  }

  /** 构建 system prompt */
  buildSystemPrompt(): string {
    const profile = this.getProfile();
    if (!profile) {
      return "";
    }

    const parts: string[] = [];

    if (profile.soul) {
      parts.push(profile.soul);
    }

    if (profile.user) {
      parts.push(profile.user);
    }

    if (profile.workspace) {
      parts.push(profile.workspace);
    }

    if (profile.memory) {
      parts.push(profile.memory);
    }

    // 注入 profile 目录路径，让 Agent 知道文件在哪里
    const profileDir = this.getProfileDir();
    parts.push(`## Profile Directory\n\nYour profile files are located at: \`${profileDir}\`\n\nUse \`edit\` or \`write\` tools to update these files when needed.`);

    return parts.join("\n\n");
  }

  /** 获取 tools 配置 */
  getToolsConfig(): ToolsConfig | undefined {
    const profile = this.getProfile();
    return profile?.config?.tools;
  }

  /** 获取完整的 profile config */
  getProfileConfig(): ProfileConfig | undefined {
    const profile = this.getProfile();
    return profile?.config;
  }
}
