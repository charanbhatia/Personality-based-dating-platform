-- Person B — F05/F06/F16: rich profile fields, photo gallery, canonical gender.
-- Forward-only. Safe to re-run.

ALTER TABLE profiles ADD COLUMN IF NOT EXISTS interests          TEXT[] NOT NULL DEFAULT '{}';
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS photo_urls         JSONB  NOT NULL DEFAULT '[]'::jsonb;
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS primary_photo_url  TEXT;

-- bio/gender/location were nullable, forcing COALESCE into every read. Collapse
-- NULL to '' once and make absence explicit instead.
UPDATE profiles SET bio      = '' WHERE bio IS NULL;
UPDATE profiles SET gender   = '' WHERE gender IS NULL;
UPDATE profiles SET location = '' WHERE location IS NULL;

ALTER TABLE profiles ALTER COLUMN bio      SET DEFAULT '';
ALTER TABLE profiles ALTER COLUMN gender   SET DEFAULT '';
ALTER TABLE profiles ALTER COLUMN location SET DEFAULT '';
ALTER TABLE profiles ALTER COLUMN bio      SET NOT NULL;
ALTER TABLE profiles ALTER COLUMN gender   SET NOT NULL;
ALTER TABLE profiles ALTER COLUMN location SET NOT NULL;

-- Canonicalize gender to the tokens in internal/domain/gender.go. The seed
-- previously wrote 'Male'/'Female'/'Non-binary'; discovery compares these
-- values literally, so drift here silently empties the feed.
UPDATE profiles
SET gender = CASE lower(btrim(gender))
        WHEN ''           THEN ''
        WHEN 'man'        THEN 'man'
        WHEN 'male'       THEN 'man'
        WHEN 'm'          THEN 'man'
        WHEN 'woman'      THEN 'woman'
        WHEN 'female'     THEN 'woman'
        WHEN 'f'          THEN 'woman'
        WHEN 'nonbinary'  THEN 'nonbinary'
        WHEN 'non-binary' THEN 'nonbinary'
        WHEN 'non binary' THEN 'nonbinary'
        WHEN 'nb'         THEN 'nonbinary'
        WHEN 'enby'       THEN 'nonbinary'
        ELSE 'other'
    END
WHERE gender <> CASE lower(btrim(gender))
        WHEN ''           THEN ''
        WHEN 'man'        THEN 'man'
        WHEN 'male'       THEN 'man'
        WHEN 'm'          THEN 'man'
        WHEN 'woman'      THEN 'woman'
        WHEN 'female'     THEN 'woman'
        WHEN 'f'          THEN 'woman'
        WHEN 'nonbinary'  THEN 'nonbinary'
        WHEN 'non-binary' THEN 'nonbinary'
        WHEN 'non binary' THEN 'nonbinary'
        WHEN 'nb'         THEN 'nonbinary'
        WHEN 'enby'       THEN 'nonbinary'
        ELSE 'other'
    END;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_profiles_gender'
    ) THEN
        ALTER TABLE profiles ADD CONSTRAINT chk_profiles_gender
            CHECK (gender IN ('', 'man', 'woman', 'nonbinary', 'other'));
    END IF;
END $$;

-- Gallery is the source of truth; photo_url stays in sync as the legacy alias
-- for the pre-v1 API and the current frontend.
UPDATE profiles
SET photo_urls = jsonb_build_array(photo_url)
WHERE photo_urls = '[]'::jsonb AND coalesce(photo_url, '') <> '';

UPDATE profiles
SET primary_photo_url = photo_urls->>0
WHERE primary_photo_url IS NULL AND jsonb_array_length(photo_urls) > 0;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_profiles_photo_urls_array'
    ) THEN
        ALTER TABLE profiles ADD CONSTRAINT chk_profiles_photo_urls_array
            CHECK (jsonb_typeof(photo_urls) = 'array');
    END IF;
END $$;

-- Discovery filters candidates by gender.
CREATE INDEX IF NOT EXISTS idx_profiles_gender ON profiles (gender) WHERE gender <> '';

-- Every user must have a profile row; discovery inner-joins it. Backfill any
-- user that predates the transactional registration path.
INSERT INTO profiles (user_id, bio, gender, location, photo_url, created_at, updated_at)
SELECT u.id, '', '', '', '', now(), now()
FROM users u
WHERE NOT EXISTS (SELECT 1 FROM profiles p WHERE p.user_id = u.id);
