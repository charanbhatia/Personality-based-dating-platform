-- Person C — F22: device tokens for FCM / APNs / web push.
-- Forward-only. Safe to re-run.

CREATE TABLE IF NOT EXISTS device_tokens (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token      TEXT NOT NULL,
    platform   TEXT NOT NULL CHECK (platform IN ('web', 'android', 'ios')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, token)
);

CREATE INDEX IF NOT EXISTS idx_device_tokens_user ON device_tokens (user_id);
