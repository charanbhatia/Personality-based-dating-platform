-- Person B — F07/F08: Big Five question bank, scoring version, retake support.
-- Forward-only. Safe to re-run (question upsert is keyed on `code`).

ALTER TABLE personality_scores ADD COLUMN IF NOT EXISTS version     INT NOT NULL DEFAULT 1;
ALTER TABLE personality_scores ADD COLUMN IF NOT EXISTS assessed_at TIMESTAMPTZ;
ALTER TABLE personality_scores ADD COLUMN IF NOT EXISTS raw_answers JSONB;

UPDATE personality_scores SET assessed_at = updated_at WHERE assessed_at IS NULL;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint WHERE conname = 'chk_personality_scores_traits_object'
    ) THEN
        ALTER TABLE personality_scores ADD CONSTRAINT chk_personality_scores_traits_object
            CHECK (jsonb_typeof(traits) = 'object');
    END IF;
END $$;

CREATE TABLE IF NOT EXISTS personality_questions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    -- Stable business key so re-running the seed updates wording in place
    -- rather than duplicating the bank.
    code       TEXT NOT NULL,
    prompt     TEXT NOT NULL,
    trait_key  TEXT NOT NULL,
    -- 1 = agreement raises the trait, -1 = reverse scored.
    direction  SMALLINT NOT NULL DEFAULT 1,
    sort_order INT NOT NULL,
    active     BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT chk_personality_questions_trait CHECK (
        trait_key IN ('openness', 'conscientiousness', 'extraversion', 'agreeableness', 'neuroticism')
    ),
    CONSTRAINT chk_personality_questions_direction CHECK (direction IN (1, -1))
);

CREATE UNIQUE INDEX IF NOT EXISTS uq_personality_questions_code ON personality_questions (code);
CREATE INDEX IF NOT EXISTS idx_personality_questions_active ON personality_questions (sort_order)
    WHERE active;

-- 30 items: six per trait, three keyed forward and three reverse scored so
-- acquiescence bias cancels out. sort_order interleaves traits so the quiz does
-- not read as five themed blocks.
INSERT INTO personality_questions (code, prompt, trait_key, direction, sort_order) VALUES
    ('op_01', 'I enjoy ideas that challenge the way I see the world.',              'openness',          1,   1),
    ('co_01', 'I finish what I start, even after it stops being fun.',              'conscientiousness', 1,   2),
    ('ex_01', 'I feel energised after spending time in a group.',                   'extraversion',      1,   3),
    ('ag_01', 'I go out of my way to make other people comfortable.',               'agreeableness',     1,   4),
    ('ne_01', 'Small setbacks can unsettle me for the rest of the day.',            'neuroticism',       1,   5),
    ('op_02', 'I would rather stick with what I know than experiment.',             'openness',         -1,   6),
    ('co_02', 'I put things off until a deadline forces me to act.',                'conscientiousness',-1,   7),
    ('ex_02', 'Large gatherings leave me drained.',                                 'extraversion',     -1,   8),
    ('ag_02', 'I say what I think even when it stings.',                            'agreeableness',    -1,   9),
    ('ne_02', 'I stay calm when plans fall apart.',                                 'neuroticism',      -1,  10),
    ('op_03', 'Art, music, or writing that makes me think stays with me.',          'openness',          1,  11),
    ('co_03', 'I plan ahead instead of deciding at the last minute.',               'conscientiousness', 1,  12),
    ('ex_03', 'I start conversations with people I have just met.',                 'extraversion',      1,  13),
    ('ag_03', 'I assume people mean well until they show me otherwise.',            'agreeableness',     1,  14),
    ('ne_03', 'I worry about things that may never happen.',                        'neuroticism',       1,  15),
    ('op_04', 'Abstract or philosophical conversations tend to bore me.',           'openness',         -1,  16),
    ('co_04', 'I leave tasks unfinished more often than I would like.',             'conscientiousness',-1,  17),
    ('ex_04', 'I stay quiet until somebody speaks to me first.',                    'extraversion',     -1,  18),
    ('ag_04', 'In a disagreement I put my own needs first.',                        'agreeableness',    -1,  19),
    ('ne_04', 'I rarely feel anxious.',                                             'neuroticism',      -1,  20),
    ('op_05', 'I like trying food, places, and hobbies I have never tried.',        'openness',          1,  21),
    ('co_05', 'I keep my commitments, including the small ones.',                   'conscientiousness', 1,  22),
    ('ex_05', 'I would rather be out with people than home on my own.',             'extraversion',      1,  23),
    ('ag_05', 'I find it easy to forgive.',                                         'agreeableness',     1,  24),
    ('ne_05', 'My mood shifts more than most people''s.',                           'neuroticism',       1,  25),
    ('op_06', 'I prefer routines that rarely change.',                              'openness',         -1,  26),
    ('co_06', 'My space is usually a mess.',                                        'conscientiousness',-1,  27),
    ('ex_06', 'I need a lot of time alone to recharge.',                            'extraversion',     -1,  28),
    ('ag_06', 'I am slow to trust somebody new.',                                   'agreeableness',    -1,  29),
    ('ne_06', 'Criticism does not shake my confidence.',                            'neuroticism',      -1,  30)
ON CONFLICT (code) DO UPDATE SET
    prompt     = EXCLUDED.prompt,
    trait_key  = EXCLUDED.trait_key,
    direction  = EXCLUDED.direction,
    sort_order = EXCLUDED.sort_order,
    active     = true,
    updated_at = now();
