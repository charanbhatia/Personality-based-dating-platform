-- Person B — F01 leftover: email verification tokens.
-- Forward-only. Safe to re-run.

CREATE TABLE IF NOT EXISTS email_verification_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    used_at    TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_email_verification_token ON email_verification_tokens (token_hash);
CREATE INDEX IF NOT EXISTS idx_email_verification_open ON email_verification_tokens (user_id)
    WHERE used_at IS NULL;
