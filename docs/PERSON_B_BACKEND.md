# Person B — Backend: Auth, Profile, Personality & Matching

**Role:** Own identity, profiles, personality assessment, preferences, discovery/matching, likes, blocks/reports.  
**Does not own:** WebSocket gateway, object storage, notification workers, Docker/CI, Redis ops (consume via C’s `platform/`).  
**Collaborates with:** A (API consumer), C (events, media URLs, shared middleware).  
**Source of truth:** [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md).

---

## 1. Purpose

This document is the complete backend playbook for Person B: gap analysis, feature specs, schemas, endpoints, matching algorithm v2, domain-level scaling, tests, milestones, and handoffs to C.

---

## 2. Current backend baseline (your domain)

> **Status:** the table below is the original gap analysis, kept for context. Most
> of it is now built — see the [milestone checklist](#9-milestone-checklist-b) for
> what is done and what is still blocked, and
> [PERSON_B_API.md](./PERSON_B_API.md) for the implemented contract.

| Area | Exists | Gap |
|------|--------|-----|
| Register / login | JWT HS256 24h, bcrypt | No refresh, logout revoke, password reset, sessions |
| `/api/auth/me` | Yes | No onboarding flags |
| Profile | bio, gender, location, photo_url | No interests, gallery, DOB update path clarity |
| Personality | Table + `GetPersonality` / `UpsertPersonality`; seed fills traits | **No quiz API**; new users have empty traits → match score 0.5 |
| Preferences | Table + model | **Unused** — no handlers |
| Matching | List candidates + in-process similarity | Not sorted by score; no filters; **returns email**; no likes/mutual match |
| Blocks / reports | None | Needed for safety MVP |
| Packages | `handlers`, `repository`, `auth`, `models` | Split into domain packages under `internal/` |

Key files today:

- [`backend/internal/router/router.go`](../backend/internal/router/router.go)
- [`backend/internal/handlers/match.go`](../backend/internal/handlers/match.go) — similarity function
- [`backend/migrations/001_init.sql`](../backend/migrations/001_init.sql)
- [`backend/cmd/seed/main.go`](../backend/cmd/seed/main.go)

---

## 3. Owned package layout

```
backend/internal/
  auth/           # expand: tokens, password reset entities
  profile/
  personality/
  preferences/
  matching/       # discover, likes, matches, blocks, reports
```

Handlers register on the shared mux; C owns router wiring conventions and `/api/v1` mount. You provide `RegisterRoutes(r *mux.Router, deps Deps)` per domain.

**Shared (C-owned, you use):**

- `platform/db`, `platform/redis`, `platform/middleware` (JWT parse, rate limit, request ID)
- `platform/queue` publisher helper for events

**You define:** JWT claims shape (`user_id`, `sid`, `exp`, `typ=access|refresh`).

---

## 4. Data model (B-owned tables)

### 4.1 Extend existing

**users** — keep; add optional `email_verified_at`.

**profiles** — add:

- `interests TEXT[]` or JSONB
- `photo_urls JSONB` (array of strings) — replace single `photo_url` gradually
- `primary_photo_url TEXT` denormalized for feed performance

**personality_scores** — keep `traits JSONB`; add:

- `version INT` (scoring version)
- `assessed_at TIMESTAMPTZ`
- `raw_answers JSONB` (optional, privacy-sensitive — retain only if product needs retake analytics)

**preferences** — activate:

- `age_min`, `age_max`
- `genders TEXT[]` (sought)
- `max_distance_km INT` (nullable = anywhere)
- `trait_weights JSONB` (optional multipliers per Big Five key; default 1.0)
- `lat`, `lng` **or** rely on profile location string initially (distance can be Phase-lite: skip geo until M3 if needed; document decision: **M2 ship age/gender/trait filters; distance optional stub**)

### 4.2 New tables

```sql
-- refresh sessions
CREATE TABLE auth_sessions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  refresh_token_hash TEXT NOT NULL,
  user_agent TEXT,
  ip INET,
  expires_at TIMESTAMPTZ NOT NULL,
  revoked_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_auth_sessions_user ON auth_sessions(user_id);

-- password reset tokens
CREATE TABLE password_reset_tokens (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL,
  expires_at TIMESTAMPTZ NOT NULL,
  used_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- assessment question bank (static seed OK)
CREATE TABLE personality_questions (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  prompt TEXT NOT NULL,
  trait_key TEXT NOT NULL, -- openness|conscientiousness|extraversion|agreeableness|neuroticism
  direction SMALLINT NOT NULL DEFAULT 1, -- 1 or -1 for reverse scored
  sort_order INT NOT NULL
);

-- likes / passes
CREATE TABLE swipes (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  from_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  action TEXT NOT NULL CHECK (action IN ('like', 'pass')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (from_user_id, to_user_id)
);
CREATE INDEX idx_swipes_to_like ON swipes(to_user_id) WHERE action = 'like';

-- mutual matches
CREATE TABLE matches (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_a_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_b_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (user_a_id, user_b_id),
  CHECK (user_a_id < user_b_id)
);
CREATE INDEX idx_matches_a ON matches(user_a_id);
CREATE INDEX idx_matches_b ON matches(user_b_id);

CREATE TABLE blocks (
  blocker_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (blocker_id, blocked_id)
);

CREATE TABLE reports (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  reporter_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reported_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  reason TEXT NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

Migration files: `002_b_auth_sessions.sql`, `003_b_personality_questions.sql`, `004_b_swipes_matches.sql`, etc.

---

## 5. Feature specifications

### 5.1 Auth (F01–F04, F03)

#### Endpoints

| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/auth/register` | No | Create user + empty profile + empty prefs row |
| POST | `/api/v1/auth/login` | No | Return tokens |
| POST | `/api/v1/auth/refresh` | Refresh token | Rotate refresh; new access |
| POST | `/api/v1/auth/logout` | Access | Revoke current session |
| GET | `/api/v1/auth/me` | Access | User + onboarding flags |
| POST | `/api/v1/auth/password/forgot` | No | Always 202; enqueue email event |
| POST | `/api/v1/auth/password/reset` | No | token + new password |
| POST | `/api/v1/auth/email/verify` | No | token; sets `email_verified_at` |
| POST | `/api/v1/auth/email/resend` | Access | new confirmation token |
| GET | `/api/v1/auth/sessions` | Access | List sessions |
| DELETE | `/api/v1/auth/sessions/:id` | Access | Revoke |

#### Token rules

- **Access:** JWT, 15 minutes, claims `typ=access`, `user_id`, `sid`
- **Refresh:** opaque random (32+ bytes) stored **hashed** in `auth_sessions`, 30 days; rotate on use
- Optional: also cache session revoke list in Redis (C) for fast logout propagation — call `platform/redis`. **Not implemented.** Logout revokes the refresh session immediately; the access JWT is short-lived (15m) and is not denylisted.

#### `/me` response

```json
{
  "user": { "id": "...", "email": "...", "name": "...", "date_of_birth": "..." },
  "onboarding": {
    "quiz_done": true,
    "preferences_done": true,
    "profile_done": true,
    "photos_done": false
  }
}
```

Flags derived from DB (traits present, prefs row filled, profile bio/gender set, `photo_urls` length ≥ 1).

#### Validation

- Email unique, normalized lowercase
- Password min 8 chars (document policy)
- Rate limit login/register/forgot via C middleware (keys by IP + email)

#### Password reset

1. Create hashed token, TTL 1h
2. Publish `auth.password_reset_requested` for C email worker
3. Reset endpoint verifies hash, updates password, revokes all sessions

---

### 5.2 Profile (F05–F06, F16)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/profile` | Own profile |
| PUT | `/api/v1/profile` | Update bio, gender, location, interests, DOB |
| PUT | `/api/v1/profile/photos` | Body: `{ "asset_ids": ["..."] }` or `{ "photo_urls": ["..."] }` ordered |
| GET | `/api/v1/users/:id/public` | Public card for matched/discoverable users |

**Public DTO must never include email.**

Photos: prefer accepting `asset_ids` from C media; resolve to URLs via media table join or callback `media.processed`. Until media ready, accept HTTPS URLs (legacy).

---

### 5.3 Personality assessment (F07–F08)

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/personality/assessment` | Question list |
| POST | `/api/v1/personality/assessment/submit` | Answers → compute traits → upsert |
| GET | `/api/v1/personality/me` | Current traits + assessed_at + next_retake_at |

#### Scoring

- Likert 1–5 per question
- Map to trait via `trait_key` and `direction`
- Normalize each trait to **0.0–1.0** float
- Persist in `personality_scores.traits`

Example shape:

```json
{
  "openness": 0.72,
  "conscientiousness": 0.55,
  "extraversion": 0.41,
  "agreeableness": 0.63,
  "neuroticism": 0.38
}
```

#### Retake (F08)

- Default: allow retake after **30 days** (config)
- Before that: `409` with `next_retake_at`

Seed ≥ 25 questions via migration or seed command (extend `cmd/seed`).

---

### 5.4 Preferences (F09)

| Method | Path |
|--------|------|
| GET | `/api/v1/preferences` |
| PUT | `/api/v1/preferences` |

Body:

```json
{
  "age_min": 23,
  "age_max": 35,
  "genders": ["woman", "nonbinary"],
  "max_distance_km": 50,
  "trait_weights": {
    "openness": 1.2,
    "conscientiousness": 1.0,
    "extraversion": 0.8,
    "agreeableness": 1.1,
    "neuroticism": 0.7
  }
}
```

`preferences_done` = age range set + at least one gender (define clearly in code).

`max_distance_km` is accepted and stored so the UI can collect it. It is **not**
applied to discovery (see package comment on `internal/preferences`). Trait
weights **are** applied as a weighted average in the discover SQL.

---

### 5.5 Discovery & matching (F10–F13)

#### Discover

```http
GET /api/v1/discover?limit=20&cursor=
```

**Algorithm v2**

1. Load viewer traits + preferences + block lists + prior swipes.
2. Candidate query (SQL):
   - Exclude self, blocked (either direction), already swiped
   - Filter age from `date_of_birth` if present
   - Filter gender if preferences set
   - Require candidate has `personality_scores` row (optional soft: include with score 0.5 — **prefer require traits** for MVP quality)
3. Score in app or SQL:
   - Base: average over traits of `(1 - |a-b|)` (same as current `similarity`)
   - Apply `trait_weights`: weighted average
   - Clamp 0–1
4. **Sort by score DESC**, then `user_id` for stability
5. Cursor: opaque `(score, user_id)` encoded (base64 JSON)

**Caching (domain scaling)**

- Redis key `traits:{user_id}` TTL 1h — invalidate on assessment submit
- Redis key `discover:prefetch:{user_id}` optional short TTL (30–60s) for first page only
- Do not cache pages that include swipe state without invalidation on swipe

#### Like / pass

```http
POST /api/v1/likes
{ "user_id": "...", "action": "like" | "pass" }
```

Response:

```json
{ "ok": true, "matched": true, "match_id": "..." }
```

On mutual like (other user already liked viewer):

1. Insert `matches` with ordered UUIDs
2. Publish `match.created` for C
3. Return `matched: true`

Idempotent: duplicate swipe returns prior result without error.

#### Match list

```http
GET /api/v1/matches?limit=&cursor=
```

Items: match_id, other user public profile, created_at, compatibility score (recompute or store at match time).

---

### 5.6 Block & report (F14–F15)

```http
POST /api/v1/blocks   { "user_id": "..." }
DELETE /api/v1/blocks/{user_id}
GET /api/v1/blocks
POST /api/v1/reports  { "user_id": "...", "reason": "..." }
```

On block: publish `user.blocked` so C can hide conversations. Exclude from discover immediately.

---

## 6. Contracts with C

| You provide | C uses |
|-------------|--------|
| `match.created` event | Notifications; optional conversation bootstrap |
| `user.blocked` event | Messaging ACL / hide |
| `auth.password_reset_requested` | Email worker |
| JWT claim validation rules | Shared middleware |
| Profile stores photo URLs / asset refs | Written after C media processing |

| You consume | From C |
|-------------|--------|
| Rate limit middleware | All public auth routes |
| Redis client | Trait cache, optional session revoke |
| Queue publisher | Events |
| Media asset URLs | Profile photos |

**Same-DB rule (monolith):** C messaging may `SELECT` from `matches` to gate conversations. Do not break `matches` schema without notifying C. Prefer a small `matching.Service.IsMatched(ctx, a, b)` used by C in-process.

---

## 7. Domain scaling responsibilities (B)

You are not the infra owner, but you **must** design B endpoints to scale:

1. **Indexes**
   - `swipes (from_user_id, to_user_id)` unique
   - `matches (user_a_id)`, `(user_b_id)`
   - `users (date_of_birth)` if filtering age
   - `profiles (gender)`
   - GIN on `traits` only if querying JSON paths (usually not needed)

2. **Discovery cost**
   - Avoid loading all users into memory — SQL filter first, score top N candidates (e.g. fetch 5× limit, score, return limit) until a materialized score table exists
   - M4 stretch: nightly job writing `compatibility_edges` for active users — only if load tests demand it

3. **Redis**
   - Trait cache as above
   - Never use Redis as source of truth for swipes/matches

4. **Payload hygiene**
   - Strip emails from all public/match/discover DTOs (fix current PoC leak)

5. **Load-test targets (M4, with C)**
   - Discover p95 &lt; 200ms at 50 RPS warm cache for seeded 10k users (document actual numbers)

6. **Connection usage**
   - Use `pgx` pool from platform; no per-request `Connect`
   - Transactions for like + match insert + event outbox

### Outbox pattern (recommended)

To not lose `match.created`:

```sql
CREATE TABLE outbox_events (
  id UUID PRIMARY KEY,
  event_type TEXT NOT NULL,
  payload JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  published_at TIMESTAMPTZ
);
```

Write event in same transaction as match; C’s publisher worker drains outbox (or B runs a small publisher loop — **prefer C-owned publisher** reading outbox, or B calls queue in-process after commit with best-effort + outbox fallback). Agree in M2 standup: **default = in-process publish after commit + outbox retry table owned by B, drained by shared worker C runs.**

---

## 8. Testing plan (B)

| Layer | Cases | Where |
|-------|--------|-------|
| Unit | Similarity / weighted score; cursor encode/decode; trait normalize; token issue/verify; password policy; request decoding and validation; cache expiry and eviction; config validation | alongside each package, e.g. `internal/domain/traits_test.go`, `internal/matching/cursor_test.go`, `internal/auth/token_test.go` |
| Routing | 404 vs 405 with `Allow`; every route reaches its handler; CORS allowlist | `internal/router/router_test.go` |
| Integration | Register→quiz→prefs→discover order; like→mutual match→event; concurrent mutual likes; block excludes; refresh rotate and replay; reset password | `internal/integration/` |
| Regression | Public DTOs contain no `email` field, on both the v1 and legacy mounts | `internal/integration/discover_test.go`, `legacy_test.go` |

Run unit tests with `go test ./...`. The integration suite needs a real Postgres
and skips itself unless `TEST_DATABASE_URL` is set; it truncates the tables it
uses, so point it at a throwaway database. C owns CI and should add
`go test -race`, which needs a cgo toolchain.

Two details worth knowing before adding integration tests:

- The seeded question bank is balanced (equal forward and reverse items per
  trait), so answering every question with the same value always scores 0.5 on
  every trait. Use the level-based fixtures in `client_test.go`
  (`submitAssessmentPerTrait`, `traitValueForLevel`) when a test needs distinct
  trait values.
- Tests share one database and run sequentially; `resetDB` truncates between
  them. Do not add `t.Parallel()` to a test that asserts on the contents of the
  discovery feed.

---

## 9. Milestone checklist (B)

Implemented endpoints and their exact contracts are in
[PERSON_B_API.md](./PERSON_B_API.md).

### Spec coverage (required vs optional)

| Spec item | Status |
|-----------|--------|
| F01–F04 auth (access + refresh rotation/reuse, logout, sessions) | Required — done |
| F03 password reset (hashed token, 202 forgot, revoke all sessions) | Required — done |
| Email verification (`/auth/email/verify`, `/auth/email/resend`) | Required leftover — done; login is **not** gated |
| F05 profile CRUD + F16 no email on public/discover/match DTOs | Required — done |
| F06 photos: HTTPS URLs **and** C `asset_ids` | Required — done |
| F07–F08 quiz, 0–1 traits, 30-day retake, cache invalidate | Required — done |
| F09 prefs: age, gender, **trait_weights applied in SQL scoring** | Required — done |
| F09 `max_distance_km` | **Optional / deferred** — stored + returned; `DistanceFilterActive: false`; not in discover SQL (M2 decision) |
| F10–F13 discover SQL prefilter, score sort, cursor, likes, mutual match, outbox | Required — done |
| F14–F15 block/report + `user.blocked` | Required — done |
| Redis `sess:{sid}` access-token denylist | **Optional** — not built; logout revokes refresh immediately; access JWT dies at expiry (~15m) |
| `discover:prefetch:{user_id}` | **Optional** — not built; trait cache is the M2 cache |
| `Idempotency-Key` on likes | Roadmap “recommended M3” — CORS allows the header; swipe uniqueness is the idempotency key |
| Auth rate limit on **v1** register/login/forgot/refresh/verify | Required (C F24) — wired; no-op without `REDIS_URL` (fail-closed when Redis is up and errors) |
| M4 load-test / explain-analyze / cache-hit metrics | **Deferred M4** |

Error handling that is intentional, not a gap: forgot-password always 202; login uses one `invalid_credentials` and burns a bcrypt compare on unknown emails; cross-account session ids and blocked public profiles are 404 not 403; self-like/block/report are 422; unexpected errors are generic 500.

### M1

- [x] `/api/v1/auth/*` access + refresh + logout + me flags — rotation with reuse
      detection, session list/revoke, password reset
- [x] Profile GET/PUT (interests) — merge semantics, canonical genders
- [x] Preferences GET/PUT — replace semantics, validated bounds
- [x] Assessment GET/submit + seed questions — 30 balanced Big Five items in
      migration 004
- [x] Personality GET me
- [x] Remove email from any candidate DTO still under `/api/matches` shim or v1
      discover preview — public payloads share one `PublicProfile` type that has
      no email field, so a new endpoint cannot reintroduce the leak
- [x] Migrations applied; env documented for token TTLs — see
      [`backend/env.example`](../backend/env.example)

### M2

- [x] Discover scored + filtered + cursor — scored in SQL, keyset cursor
- [x] Likes/pass + mutual match + `match.created` — advisory-locked, so
      concurrent mutual likes create exactly one match and one event
- [x] Match list
- [x] Profile photos gallery with URL safety rules
- [x] Profile photos integration with C URLs — `PUT /profile/photos` with
      `asset_ids` resolves owned, ready media assets through C's media service
- [x] Trait Redis cache — `cache.Redis` is used when `REDIS_URL` is set;
      otherwise the in-process TTL cache
- [x] Integration tests for match path

### M3

- [x] Block/report + `user.blocked`
- [x] Retake rules — interval enforced, cache invalidated on retake
- [x] Password reset full path with C email stub — emits
      `auth.password_reset_requested`; C's mailer consumes it
- [x] Email verification — register emits `auth.email_verification_requested`;
      `POST /auth/email/verify` and authenticated resend; login is not gated
- [ ] Query/index tuning from explain analyze — indexes are in place from the
      migrations, but not yet validated against a realistic data volume

### M4

- [ ] Load-test discover; document results
- [ ] Cache hit metrics — C's `/metrics` exists; B's trait cache is not labelled yet
- [ ] Outbox reliability verified under worker restart — outbox now publishes
      to Redis Streams when `REDIS_URL` is set; not yet load-tested

### Verification status

| Check | State |
|-------|-------|
| `go build ./...`, `go vet ./...` | Clean |
| Unit tests (`domain`, `httpx`, `auth`, `personality`, `matching`, `cache`, `config`, `router`) | Passing |
| Integration tests against real Postgres | Passing — auth, profile, personality, preferences, discovery, matching, legacy compatibility |
| `go test -race` | **Not yet run.** Needs a cgo toolchain, which the current Windows dev box lacks; it should run in C's Linux CI. The concurrency-sensitive paths (mutual match, outbox drainer, trait cache) do have concurrent test coverage, but without the detector |
| `gofmt -l` | Clean on content. Every file in the repo uses CRLF, which `gofmt` reports as unformatted; a `.gitattributes` with `*.go text eol=lf` is the fix and is C's call as CI owner |

---

## 10. Non-ownership (do not build)

- WebSocket hub / Redis pubsub for chat
- S3/MinIO buckets, thumbnail workers
- Notification inbox tables/API
- Docker Compose, GitHub Actions, OTel collectors
- Frontend pages

If you need a platform capability, open a request for C with feature ID.

---

## 11. Risks

| Risk | Mitigation |
|------|------------|
| Scoring all users O(N) | Prefilter SQL; cap candidate window; cache traits |
| Event loss on match | Outbox table |
| Geo complexity | Defer distance filter; ship age/gender/traits first |
| Conflict with C on `matches` schema | Shared roadmap + `IsMatched` helper |
| Assessment quality | Seed validated question set; version traits |

---

## 12. Definition of Done (B)

1. Endpoints under `/api/v1` match this doc and shared conventions.
2. Migrations reversible or clearly forward-only documented.
3. Tests for match creation and discover filters pass in CI.
4. Events in shared roadmap emitted with required fields.
5. A can complete onboarding + discover + like against your API.
6. No other-user emails in JSON responses.
7. Milestone checklist items marked done.

---

## 13. References

- Implemented API contract: [PERSON_B_API.md](./PERSON_B_API.md)
- Configuration reference: [`backend/env.example`](../backend/env.example)
- Shared roadmap: [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md)
- Frontend consumer: [PERSON_A_FRONTEND.md](./PERSON_A_FRONTEND.md)
- Infra & messaging: [PERSON_C_BACKEND.md](./PERSON_C_BACKEND.md)
- Current similarity: `backend/internal/handlers/match.go`
