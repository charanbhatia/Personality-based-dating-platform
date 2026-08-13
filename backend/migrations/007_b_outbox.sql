-- Person B — transactional outbox for the B→C events in roadmap §7.
-- Forward-only. Safe to re-run.
--
-- Domain writes insert the event in the same transaction as the state change, so
-- a match can never exist without its `match.created` event. A drainer (run by
-- the API today, movable to Person C's worker) publishes and marks rows.

CREATE TABLE IF NOT EXISTS outbox_events (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type   TEXT NOT NULL,
    payload      JSONB NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Lease/backoff marker: a claim pushes this forward so a crashed publisher
    -- releases the row without a separate reaper.
    available_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    attempts     INT NOT NULL DEFAULT 0,
    last_error   TEXT,
    published_at TIMESTAMPTZ,
    CONSTRAINT chk_outbox_payload_object CHECK (jsonb_typeof(payload) = 'object')
);

-- The drainer's claim query: unpublished rows whose lease has elapsed, oldest
-- first. Partial index keeps it proportional to the backlog, not the archive.
CREATE INDEX IF NOT EXISTS idx_outbox_pending ON outbox_events (available_at, created_at)
    WHERE published_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_outbox_type_created ON outbox_events (event_type, created_at DESC);
