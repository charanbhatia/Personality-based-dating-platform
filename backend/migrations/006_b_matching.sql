-- Person B — F12/F13/F14/F15: swipes, mutual matches, blocks, reports.
-- Forward-only. Safe to re-run.
--
-- Person C reads `matches` to gate conversations (roadmap §7). Do not change its
-- shape without a contract update.

CREATE TABLE IF NOT EXISTS swipes (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    to_user_id   UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    action       TEXT NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_swipes_pair UNIQUE (from_user_id, to_user_id),
    CONSTRAINT chk_swipes_action CHECK (action IN ('like', 'pass')),
    CONSTRAINT chk_swipes_not_self CHECK (from_user_id <> to_user_id)
);

-- Reciprocal-like probe during POST /likes.
CREATE INDEX IF NOT EXISTS idx_swipes_to_user_like ON swipes (to_user_id, from_user_id)
    WHERE action = 'like';

CREATE TABLE IF NOT EXISTS matches (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_a_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    user_b_id           UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    -- Unweighted (symmetric) Big Five similarity frozen at match time. Discovery
    -- applies each viewer's personal trait weights on top; a stored score cannot
    -- be weighted because the two sides weight traits differently.
    compatibility_score NUMERIC(8, 6),
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT uq_matches_pair UNIQUE (user_a_id, user_b_id),
    -- Canonical ordering makes the pair uniqueness constraint direction-agnostic.
    CONSTRAINT chk_matches_ordered CHECK (user_a_id < user_b_id),
    CONSTRAINT chk_matches_score CHECK (
        compatibility_score IS NULL OR (compatibility_score >= 0 AND compatibility_score <= 1)
    )
);

-- Keyset pagination of GET /matches walks (created_at DESC, id DESC) per side.
CREATE INDEX IF NOT EXISTS idx_matches_a_recent ON matches (user_a_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_matches_b_recent ON matches (user_b_id, created_at DESC, id DESC);

CREATE TABLE IF NOT EXISTS blocks (
    blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (blocker_id, blocked_id),
    CONSTRAINT chk_blocks_not_self CHECK (blocker_id <> blocked_id)
);

-- Discovery and the match list exclude blocks in both directions, so the reverse
-- lookup needs its own index.
CREATE INDEX IF NOT EXISTS idx_blocks_blocked ON blocks (blocked_id, blocker_id);

CREATE TABLE IF NOT EXISTS reports (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reported_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    reason      TEXT NOT NULL,
    details     TEXT NOT NULL DEFAULT '',
    status      TEXT NOT NULL DEFAULT 'open',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_reports_not_self CHECK (reporter_id <> reported_id),
    CONSTRAINT chk_reports_reason CHECK (
        reason IN ('spam', 'harassment', 'inappropriate_content', 'fake_profile', 'underage', 'other')
    ),
    CONSTRAINT chk_reports_status CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed'))
);

CREATE INDEX IF NOT EXISTS idx_reports_reported ON reports (reported_id, created_at DESC);
-- Collapses repeat submissions from the same reporter while a case is open.
CREATE UNIQUE INDEX IF NOT EXISTS uq_reports_open_pair ON reports (reporter_id, reported_id)
    WHERE status IN ('open', 'reviewing');
