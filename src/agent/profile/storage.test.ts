import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { tmpdir } from "node:os";
import {
  getProfileDir,
  ensureProfileDir,
  profileExists,
  readProfileFile,
  writeProfileFile,
  loadProfile,
  saveProfile,
} from "./storage.js";

describe("storage", () => {
  const testBaseDir = join(tmpdir(), `multica-test-${Date.now()}`);

  beforeEach(() => {
    // Create fresh test directory
    if (existsSync(testBaseDir)) {
      rmSync(testBaseDir, { recursive: true });
    }
    mkdirSync(testBaseDir, { recursive: true });
  });

  afterEach(() => {
    // Clean up test directory
    if (existsSync(testBaseDir)) {
      rmSync(testBaseDir, { recursive: true });
    }
  });

  describe("getProfileDir", () => {
    it("should return correct path with custom baseDir", () => {
      const result = getProfileDir("test-profile", { baseDir: testBaseDir });
      expect(result).toBe(join(testBaseDir, "test-profile"));
    });

    it("should handle profile IDs with special characters", () => {
      const result = getProfileDir("profile-with-dashes", { baseDir: testBaseDir });
      expect(result).toBe(join(testBaseDir, "profile-with-dashes"));
    });
  });

  describe("ensureProfileDir", () => {
    it("should create directory if it does not exist", () => {
      const profileId = "new-profile";
      const dir = ensureProfileDir(profileId, { baseDir: testBaseDir });

      expect(existsSync(dir)).toBe(true);
      expect(dir).toBe(join(testBaseDir, profileId));
    });

    it("should not fail if directory already exists", () => {
      const profileId = "existing-profile";
      const expectedDir = join(testBaseDir, profileId);

      mkdirSync(expectedDir, { recursive: true });

      const dir = ensureProfileDir(profileId, { baseDir: testBaseDir });
      expect(dir).toBe(expectedDir);
      expect(existsSync(dir)).toBe(true);
    });
  });

  describe("profileExists", () => {
    it("should return false for non-existent profile", () => {
      const result = profileExists("non-existent", { baseDir: testBaseDir });
      expect(result).toBe(false);
    });

    it("should return true for existing profile", () => {
      const profileId = "existing";
      mkdirSync(join(testBaseDir, profileId), { recursive: true });

      const result = profileExists(profileId, { baseDir: testBaseDir });
      expect(result).toBe(true);
    });
  });

  describe("readProfileFile", () => {
    it("should return undefined for non-existent file", () => {
      const profileId = "profile";
      mkdirSync(join(testBaseDir, profileId), { recursive: true });

      const result = readProfileFile(profileId, "missing.md", { baseDir: testBaseDir });
      expect(result).toBeUndefined();
    });

    it("should return file contents for existing file", () => {
      const profileId = "profile";
      const fileName = "test.md";
      const content = "# Test Content\n\nThis is a test.";
      const dir = join(testBaseDir, profileId);

      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, fileName), content);

      const result = readProfileFile(profileId, fileName, { baseDir: testBaseDir });
      expect(result).toBe(content);
    });

    it("should return undefined for non-existent profile directory", () => {
      const result = readProfileFile("non-existent", "file.md", { baseDir: testBaseDir });
      expect(result).toBeUndefined();
    });
  });

  describe("writeProfileFile", () => {
    it("should create file in existing directory", () => {
      const profileId = "profile";
      const fileName = "test.md";
      const content = "Test content";

      writeProfileFile(profileId, fileName, content, { baseDir: testBaseDir });

      const filePath = join(testBaseDir, profileId, fileName);
      expect(existsSync(filePath)).toBe(true);
      expect(readFileSync(filePath, "utf-8")).toBe(content);
    });

    it("should create directory if it does not exist", () => {
      const profileId = "new-profile";
      const fileName = "test.md";
      const content = "Test content";

      writeProfileFile(profileId, fileName, content, { baseDir: testBaseDir });

      const filePath = join(testBaseDir, profileId, fileName);
      expect(existsSync(filePath)).toBe(true);
    });

    it("should overwrite existing file", () => {
      const profileId = "profile";
      const fileName = "test.md";
      const dir = join(testBaseDir, profileId);

      mkdirSync(dir, { recursive: true });
      writeFileSync(join(dir, fileName), "Original content");

      writeProfileFile(profileId, fileName, "New content", { baseDir: testBaseDir });

      expect(readFileSync(join(dir, fileName), "utf-8")).toBe("New content");
    });
  });

  describe("loadProfile", () => {
    it("should load all profile files", () => {
      const profileId = "full-profile";
      const dir = join(testBaseDir, profileId);
      mkdirSync(dir, { recursive: true });

      writeFileSync(join(dir, "soul.md"), "Soul content");
      writeFileSync(join(dir, "user.md"), "User content");
      writeFileSync(join(dir, "workspace.md"), "Workspace content");
      writeFileSync(join(dir, "memory.md"), "Memory content");

      const profile = loadProfile(profileId, { baseDir: testBaseDir });

      expect(profile.id).toBe(profileId);
      expect(profile.soul).toBe("Soul content");
      expect(profile.user).toBe("User content");
      expect(profile.workspace).toBe("Workspace content");
      expect(profile.memory).toBe("Memory content");
    });

    it("should return undefined for missing files", () => {
      const profileId = "partial-profile";
      const dir = join(testBaseDir, profileId);
      mkdirSync(dir, { recursive: true });

      writeFileSync(join(dir, "soul.md"), "Soul only");

      const profile = loadProfile(profileId, { baseDir: testBaseDir });

      expect(profile.id).toBe(profileId);
      expect(profile.soul).toBe("Soul only");
      expect(profile.user).toBeUndefined();
      expect(profile.workspace).toBeUndefined();
      expect(profile.memory).toBeUndefined();
    });

    it("should handle non-existent profile", () => {
      const profile = loadProfile("non-existent", { baseDir: testBaseDir });

      expect(profile.id).toBe("non-existent");
      expect(profile.soul).toBeUndefined();
    });
  });

  describe("saveProfile", () => {
    it("should save all defined profile fields", () => {
      const profile = {
        id: "save-test",
        soul: "Soul data",
        user: "User data",
        workspace: "Workspace data",
        memory: "Memory data",
      };

      saveProfile(profile, { baseDir: testBaseDir });

      const dir = join(testBaseDir, profile.id);
      expect(readFileSync(join(dir, "soul.md"), "utf-8")).toBe("Soul data");
      expect(readFileSync(join(dir, "user.md"), "utf-8")).toBe("User data");
      expect(readFileSync(join(dir, "workspace.md"), "utf-8")).toBe("Workspace data");
      expect(readFileSync(join(dir, "memory.md"), "utf-8")).toBe("Memory data");
    });

    it("should only save defined fields", () => {
      const profile = {
        id: "partial-save",
        soul: "Soul only",
        user: undefined,
        workspace: undefined,
        memory: undefined,
      };

      saveProfile(profile, { baseDir: testBaseDir });

      const dir = join(testBaseDir, profile.id);
      expect(existsSync(join(dir, "soul.md"))).toBe(true);
      expect(existsSync(join(dir, "user.md"))).toBe(false);
    });

    it("should create profile directory if needed", () => {
      const profile = {
        id: "new-save-profile",
        soul: "Content",
      };

      saveProfile(profile, { baseDir: testBaseDir });

      expect(existsSync(join(testBaseDir, profile.id))).toBe(true);
    });
  });
});
