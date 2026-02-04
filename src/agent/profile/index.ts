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
  writeProfileConfig,
  writeProfileFile,
} from "./storage.js";
import { PROFILE_FILES } from "./types.js";
import { buildSystemPrompt as buildPrompt } from "../system-prompt/index.js";

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

  /** 重新从磁盘加载 profile（清除缓存） */
  reloadProfile(): AgentProfile | undefined {
    this.profile = loadAgentProfile(this.profileId, { baseDir: this.baseDir });
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

  /** Build system prompt using the structured prompt builder */
  buildSystemPrompt(): string {
    const profile = this.getProfile();
    if (!profile) {
      return "";
    }

    return buildPrompt({
      mode: "full",
      profile: {
        soul: profile.soul,
        user: profile.user,
        workspace: profile.workspace,
        memory: profile.memory,
        config: profile.config,
      },
      profileDir: this.getProfileDir(),
    });
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

  /** 更新 tools 配置 */
  updateToolsConfig(toolsConfig: ToolsConfig): void {
    const profile = this.getOrCreateProfile(false);
    const currentConfig = profile.config ?? {};
    const newConfig: ProfileConfig = {
      ...currentConfig,
      tools: toolsConfig,
    };
    profile.config = newConfig;
    this.profile = profile;
    saveProfile({ id: this.profileId, config: newConfig }, { baseDir: this.baseDir });
  }

  /** 设置单个 tool 的启用状态 */
  setToolEnabled(toolName: string, enabled: boolean): ToolsConfig {
    const currentConfig = this.getToolsConfig() ?? {};
    const allow = new Set(currentConfig.allow ?? []);
    const deny = new Set(currentConfig.deny ?? []);

    if (enabled) {
      // Enable: add to allow, remove from deny
      allow.add(toolName);
      deny.delete(toolName);
    } else {
      // Disable: add to deny, remove from allow
      deny.add(toolName);
      allow.delete(toolName);
    }

    // Build new config object, only including non-empty arrays
    const newConfig: ToolsConfig = { ...currentConfig };
    if (allow.size > 0) {
      newConfig.allow = Array.from(allow);
    } else {
      delete newConfig.allow;
    }
    if (deny.size > 0) {
      newConfig.deny = Array.from(deny);
    } else {
      delete newConfig.deny;
    }

    this.updateToolsConfig(newConfig);
    return newConfig;
  }

  /** 获取 Agent 名称 */
  getName(): string | undefined {
    const profile = this.getProfile();
    return profile?.config?.name;
  }

  /** 更新 Agent 名称 */
  updateName(name: string): void {
    const profile = this.getOrCreateProfile(false);
    const currentConfig = profile.config ?? {};
    const newConfig: ProfileConfig = {
      ...currentConfig,
      name,
    };
    profile.config = newConfig;
    this.profile = profile;
    writeProfileConfig(this.profileId, newConfig, { baseDir: this.baseDir });

    // Also update soul.md to include the agent name
    this.updateSoulWithName(name);
  }

  /** 更新 soul.md，确保包含 Agent 名称 */
  private updateSoulWithName(name: string): void {
    const profile = this.getOrCreateProfile(true); // 确保有默认模板
    let soulContent = profile.soul ?? DEFAULT_TEMPLATES.soul;

    // 替换 soul.md 中的 Name 字段
    // 匹配 "- **Name:** xxx" 格式
    const namePattern = /- \*\*Name:\*\* .*/;
    const newNameLine = `- **Name:** ${name}`;

    if (namePattern.test(soulContent)) {
      soulContent = soulContent.replace(namePattern, newNameLine);
    } else {
      // 如果没有找到 Name 字段，在 Identity 部分后添加
      const identityPattern = /## Identity\n/;
      if (identityPattern.test(soulContent)) {
        soulContent = soulContent.replace(identityPattern, `## Identity\n\n${newNameLine}\n`);
      } else {
        // 如果没有 Identity 部分，在开头添加
        soulContent = `# Soul\n\n## Identity\n\n${newNameLine}\n\n${soulContent}`;
      }
    }

    // 保存更新后的 soul.md
    writeProfileFile(this.profileId, PROFILE_FILES.soul, soulContent, { baseDir: this.baseDir });
    // 更新缓存
    if (this.profile) {
      this.profile.soul = soulContent;
    }
  }

  /** 获取 user.md 内容 */
  getUserContent(): string | undefined {
    const profile = this.getProfile();
    return profile?.user;
  }

  /** 更新 user.md 内容 */
  updateUserContent(content: string): void {
    writeProfileFile(this.profileId, PROFILE_FILES.user, content, { baseDir: this.baseDir });
    // Update cached profile
    if (this.profile) {
      this.profile.user = content;
    }
  }

  /** 获取 Agent 风格 */
  getStyle(): string | undefined {
    const profile = this.getProfile();
    return profile?.config?.style;
  }

  /** 更新 Agent 风格 */
  updateStyle(style: string): void {
    const profile = this.getOrCreateProfile(false);
    const currentConfig = profile.config ?? {};
    const newConfig: ProfileConfig = {
      ...currentConfig,
      style: style as ProfileConfig["style"],
    };
    profile.config = newConfig;
    this.profile = profile;
    writeProfileConfig(this.profileId, newConfig, { baseDir: this.baseDir });

    // Also update soul.md to include the style
    this.updateSoulWithStyle(style);
  }

  /** 更新 soul.md，确保包含 Agent 风格 */
  private updateSoulWithStyle(style: string): void {
    const profile = this.getOrCreateProfile(true);
    let soulContent = profile.soul ?? DEFAULT_TEMPLATES.soul;

    // 替换 soul.md 中的 Style 字段
    // 匹配 "- **Style:** xxx" 格式
    const stylePattern = /- \*\*Style:\*\* .*/;
    const newStyleLine = `- **Style:** ${style}`;

    if (stylePattern.test(soulContent)) {
      soulContent = soulContent.replace(stylePattern, newStyleLine);
    } else {
      // 如果没有找到 Style 字段，在 Identity 部分的 Role 后添加
      const rolePattern = /(- \*\*Role:\*\* .*)/;
      if (rolePattern.test(soulContent)) {
        soulContent = soulContent.replace(rolePattern, `$1\n${newStyleLine}`);
      } else {
        // 如果没有 Role，尝试在 Name 后添加
        const namePattern = /(- \*\*Name:\*\* .*)/;
        if (namePattern.test(soulContent)) {
          soulContent = soulContent.replace(namePattern, `$1\n${newStyleLine}`);
        }
      }
    }

    // 保存更新后的 soul.md
    writeProfileFile(this.profileId, PROFILE_FILES.soul, soulContent, { baseDir: this.baseDir });
    // 更新缓存
    if (this.profile) {
      this.profile.soul = soulContent;
    }
  }
}
