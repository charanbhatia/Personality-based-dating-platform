-- Person B — F09: activate the preferences table (previously created but unused).
-- Forward-only. Safe to re-run.

ALTER TABLE preferences ADD COLUMN IF NOT EXISTS genders         TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE preferences ADD COLUMN IF NOT EXISTS max_distance_km INT;
ALTER TABLE preferences ADD COLUMN IF NOT EXISTS trait_weights   JSONB;
ALTER TABLE preferences ADD COLUMN IF NOT EXISTS created_at      TIMESTAMPTZ NOT NULL DEFAULT now();

-- `preferred_traits` was the unused PoC column. Carry anything stored there into
-- trait_weights and leave the old column in place for one release.
UPDATE preferences
SET trait_weights = preferred_traits
WHERE trait_weights IS NULL
  AND preferred_traits IS NOT NULL
  AND jsonb_typeof(preferred_traits) = 'object';

-- Clamp pre-existing values into the range the CHECK constraints below enforce,
-- so adding the constraints cannot fail on legacy rows.
UPDATE preferences SET age_min = 18  WHERE age_min IS NOT NULL AND age_min < 18;
UPDATE preferences SET age_max = 120 WHERE age_max IS NOT NULL AND age_max > 120;
UPDATE preferences SET age_max = age_min
WHERE age_min IS NOT NULL AND age_max IS NOT NULL AND age_max < age_min;
UPDATE preferences SET max_distance_km = NULL WHERE max_distance_km IS NOT NULL AND max_distance_km < 1;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_preferences_age_bounds') THEN
        ALTER TABLE preferences ADD CONSTRAINT chk_preferences_age_bounds CHECK (
            (age_min IS NULL OR (age_min >= 18 AND age_min <= 120)) AND
            (age_max IS NULL OR (age_max >= 18 AND age_max <= 120)) AND
            (age_min IS NULL OR age_max IS NULL OR age_min <= age_max)
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_preferences_distance') THEN
        ALTER TABLE preferences ADD CONSTRAINT chk_preferences_distance CHECK (
            max_distance_km IS NULL OR (max_distance_km >= 1 AND max_distance_km <= 20000)
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_preferences_trait_weights') THEN
        ALTER TABLE preferences ADD CONSTRAINT chk_preferences_trait_weights CHECK (
            trait_weights IS NULL OR jsonb_typeof(trait_weights) = 'object'
        );
    END IF;

    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_preferences_genders') THEN
        ALTER TABLE preferences ADD CONSTRAINT chk_preferences_genders CHECK (
            genders <@ ARRAY['man', 'woman', 'nonbinary', 'other']::text[]
        );
    END IF;
END $$;

-- Registration now creates this row in the same transaction as the user. Backfill
-- users that predate it so GET /preferences never 404s on an existing account.
INSERT INTO preferences (user_id, age_min, age_max, created_at, updated_at)
SELECT u.id, 18, 99, now(), now()
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM preferences p WHERE p.user_id = u.id);
