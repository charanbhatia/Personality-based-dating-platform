-- Person B — F09: profile coordinates so discovery can apply max_distance_km.
-- Forward-only. Safe to re-run.

ALTER TABLE profiles ADD COLUMN IF NOT EXISTS lat DOUBLE PRECISION;
ALTER TABLE profiles ADD COLUMN IF NOT EXISTS lng DOUBLE PRECISION;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_profiles_lat') THEN
        ALTER TABLE profiles ADD CONSTRAINT chk_profiles_lat
            CHECK (lat IS NULL OR (lat >= -90 AND lat <= 90));
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'chk_profiles_lng') THEN
        ALTER TABLE profiles ADD CONSTRAINT chk_profiles_lng
            CHECK (lng IS NULL OR (lng >= -180 AND lng <= 180));
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_profiles_coords ON profiles (lat, lng)
    WHERE lat IS NOT NULL AND lng IS NOT NULL;
