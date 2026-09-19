-- LifeOS 本机模式只有一位人类用户。凭据与普通 Multica 身份分开保存，
-- 避免开放邮箱验证码或注册流程；密码只保存带随机盐的 PBKDF2 摘要。
CREATE TABLE IF NOT EXISTS lifeos_local_credential (
    singleton smallint PRIMARY KEY DEFAULT 1 CHECK (singleton = 1),
    username text NOT NULL UNIQUE CHECK (char_length(username) BETWEEN 3 AND 64),
    password_salt bytea NOT NULL CHECK (octet_length(password_salt) = 32),
    password_hash bytea NOT NULL CHECK (octet_length(password_hash) = 32),
    password_iterations integer NOT NULL CHECK (password_iterations >= 100000),
    failed_attempts integer NOT NULL DEFAULT 0 CHECK (failed_attempts >= 0),
    locked_until timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);
