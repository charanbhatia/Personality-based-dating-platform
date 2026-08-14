-- Conversations gain a link to the mutual match that authorises them, plus
-- denormalised activity columns so the inbox does not need a per-row join.
ALTER TABLE conversations
    ADD COLUMN IF NOT EXISTS match_id UUID,
    ADD COLUMN IF NOT EXISTS last_message_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS last_message_preview TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_conversations_match
    ON conversations(match_id) WHERE match_id IS NOT NULL;

-- The inbox orders by last activity, falling back to creation time for
-- conversations that have no messages yet.
CREATE INDEX IF NOT EXISTS idx_conversations_user1_activity
    ON conversations(user1_id, (COALESCE(last_message_at, created_at)) DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_conversations_user2_activity
    ON conversations(user2_id, (COALESCE(last_message_at, created_at)) DESC, id DESC);

-- matches is owned by Person B and may not exist yet; attach the foreign key
-- only once that table has landed.
DO $$
BEGIN
    IF to_regclass('public.matches') IS NOT NULL
       AND NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'fk_conversations_match')
    THEN
        ALTER TABLE conversations
            ADD CONSTRAINT fk_conversations_match
            FOREIGN KEY (match_id) REFERENCES matches(id) ON DELETE SET NULL;
    END IF;
END $$;

-- client_msg_id makes sends idempotent across retries and websocket replays.
ALTER TABLE messages
    ADD COLUMN IF NOT EXISTS client_msg_id TEXT;

CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_client
    ON messages(conversation_id, sender_id, client_msg_id)
    WHERE client_msg_id IS NOT NULL;

CREATE INDEX IF NOT EXISTS idx_messages_conversation_recent
    ON messages(conversation_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS conversation_reads (
    conversation_id UUID NOT NULL REFERENCES conversations(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (conversation_id, user_id)
);
