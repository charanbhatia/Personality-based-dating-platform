-- Person B — widen the legacy photo_url column.
-- Forward-only. Safe to re-run.
--
-- photo_url mirrors photo_urls[0] for the pre-v1 API. It was varchar(512) while
-- the gallery accepts URLs up to 1024 characters, so a long signed media URL
-- would be stored in the gallery and then fail the mirror write with a value-too-
-- long error. TEXT removes the mismatch; primary_photo_url is already TEXT.

ALTER TABLE profiles ALTER COLUMN photo_url TYPE TEXT;
ALTER TABLE profiles ALTER COLUMN photo_url SET DEFAULT '';

UPDATE profiles SET photo_url = '' WHERE photo_url IS NULL;

ALTER TABLE profiles ALTER COLUMN photo_url SET NOT NULL;
