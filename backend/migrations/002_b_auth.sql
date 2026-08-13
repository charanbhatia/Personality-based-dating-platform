-- Person B — F01/F02/F03: refresh sessions, password reset, email hygiene.
-- Forward-only. Safe to re-run.

ALTER TABLE users ADD COLUMN IF NOT EXISTS email_verified_at TIMESTAMPTZ;

-- Emails are matched case-insensitively from now on. Lowercase existing rows,
-- skipping any that would collide, so a genuine duplicate surfaces as a failed
-- unique index below instead of being silently merged.
UPDATE users u
SET email = lower(u.email)
WHERE u.email <> lower(u.email)
  AND NOT EXISTS (
    SELECT 1 FROM users o
    WHERE o.id <> u.id AND lower(o.email) = lower(u.email)
  );

CREATE UNIQUE INDEX IF NOT EXISTS uq_users_email_lower ON users (lower(email));

-- Age filtering in discovery reads date_of_birth.
CREATE INDEX IF NOT EXISTS idx_users_date_of_birth ON users (date_of_birth)
    WHERE date_of_birth IS NOT NULL;

-- One row per issued refresh token family. On rotation the row is updated in
-- place: the retired hash moves to previous_token_hash so replay of a
-- superseded token is detectable (see auth.Service.Refresh).
CREATE TABLE IF NOT EXISTS auth_sessions (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    refresh_token_hash  TEXT NOT NULL,
    previous_token_hash TEXT,
    rotation_count      INT NOT NULL DEFAULT 0,
    user_agent          TEXT NOT NULL DEFAULT '',
    ip                  INET,
    expires_at          TIMESTAMPTZ NOT NULL,
    revoked_at          TIMESTAMPTZ,
    revoked_reason      TEXT,
    last_used_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_auth_sessions_user ON auth_sessions (user_id);
CREATE UNIQUE INDEX IF NOT EXISTS uq_auth_sessions_token ON auth_sessions (refresh_token_hash);
CREATE INDEX IF NOT EXISTS idx_auth_sessions_previous_token ON auth_sessions (previous_token_hash)
    WHERE previous_token_hash IS NOT NULL;
-- Supports the expired-session sweep.
CREATE INDEX IF NOT EXISTS idx_auth_sessions_expires ON auth_sessions (expires_at)
    WHERE revoked_at IS NULL;

CREATE TABLE IF NOT EXISTS password_reset_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_password_reset_token ON password_reset_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_password_reset_open ON password_reset_tokens (user_id)
    WHERE used_at IS NULL;
