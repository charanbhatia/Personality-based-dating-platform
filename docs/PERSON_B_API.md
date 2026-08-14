# Person B — API Reference

The implemented contract for the auth, profile, personality, preferences and
matching endpoints. Written for Person A (frontend) and Person C (platform);
[PERSON_B_BACKEND.md](./PERSON_B_BACKEND.md) covers the design and rationale
behind it.

Everything here is verified by the test suite under `backend/internal/integration`,
so treat a disagreement between this document and the code as a bug in one of them.

---

## 1. Conventions

**Base path.** `/api/v1`. The pre-v1 `/api` endpoints still work and are
documented in §9, but they are deprecated.

**Authentication.** `Authorization: Bearer <access_token>` on every endpoint
except `/health`, register, login, refresh, the two password-reset endpoints
and `POST /auth/email/verify`.

**Content type.** Request bodies must be JSON. A `Content-Type` header is
optional, but if present it must be `application/json` (a charset parameter is
accepted). Bodies are capped at 1 MiB.

**Errors.** Every failure uses one envelope:

```json
{ "error": { "code": "validation_failed", "message": "...", "details": { "age_min": "is required" } } }
```

Branch on `code`, never on `message`. `details` is present on field validation
failures and maps field name to the first problem found with it.

| Code | Status | Meaning |
|------|--------|---------|
| `bad_request` | 400 | Malformed body, bad query parameter or bad path variable |
| `validation_failed` | 422 | Well-formed request, unacceptable field values; see `details` |
| `unauthorized` | 401 | Missing credentials |
| `token_expired` | 401 | Access token expired — refresh and retry |
| `token_invalid` | 401 | Signature, issuer or token type wrong — sign in again |
| `invalid_credentials` | 401 | Wrong email or password |
| `refresh_token_invalid` | 401 | Unknown, expired or revoked refresh token |
| `refresh_token_reused` | 401 | Replay detected; the session was revoked |
| `reset_token_invalid` | 400 | Unknown, expired or already-used reset token |
| `verify_token_invalid` | 400 | Unknown, expired or already-used email verification token |
| `forbidden` | 403 | Authenticated but not permitted |
| `not_found` | 404 | No such resource, or one hidden from this caller |
| `conflict` | 409 | State collision |
| `email_already_registered` | 409 | Email taken |
| `retake_too_soon` | 409 | Assessment retake window has not elapsed |
| `blocked_by_you` | 409 | Unblock the user before interacting |
| `assessment_required` | 409 | Take the personality assessment first |
| `asset_not_ready` | 409 | Photo `asset_ids` refer to an upload that has not finished processing |
| `cannot_swipe_self` / `cannot_block_self` / `cannot_report_self` | 422 | Self-targeted action |
| `invalid_action` | 422 | Swipe action outside `like` / `pass` |
| `underage` | 422 | Date of birth below the minimum age |
| `media_unavailable` | 501 | Photo asset ids submitted but the media service is not wired |
| `method_not_allowed` | 405 | Wrong verb; the `Allow` header lists the right ones |
| `unsupported_media_type` | 415 | `Content-Type` was not JSON |
| `payload_too_large` | 413 | Body over 1 MiB |
| `internal_error` | 500 | Server fault; details are logged, never returned |

**Pagination.** Feeds return `{ "items": [...], "next_cursor": "..." }`. Pass the
cursor back verbatim as `?cursor=`; `next_cursor` is `null` on the last page.
Cursors are opaque, single-purpose and tied to the issuing user — do not parse or
share them.

**Note on 422 vs 400.** A body that will not parse is a 400. A body that parses
but carries unacceptable values is a 422 with `details`. Clients that render
per-field errors only need the 422 case.

---

## 2. Auth

### `POST /auth/register` → 201

```json
{ "email": "ada@example.com", "password": "correct horse battery", "name": "Ada", "date_of_birth": "1994-05-05" }
```

`date_of_birth` is `YYYY-MM-DD` and the user must be at least 18. Email is
lowercased and trimmed; passwords are 8–72 characters.

Registration also creates the profile and preferences rows, so every subsequent
`GET` returns an object rather than a 404.

Response — and the response to login and refresh — is:

```json
{
  "user": {
    "id": "uuid", "email": "ada@example.com", "name": "Ada",
    "date_of_birth": "1994-05-05", "age": 32, "email_verified": false,
    "created_at": "2026-08-14T00:00:00Z"
  },
  "access_token": "jwt", "token_type": "Bearer", "expires_in": 900,
  "access_token_expires_at": "2026-08-14T00:15:00Z",
  "refresh_token": "opaque", "refresh_token_expires_at": "2026-09-13T00:00:00Z",
  "session_id": "uuid"
}
```

Errors: `422` for any invalid field, `409 email_already_registered`.

Registration also enqueues `auth.email_verification_requested`. Login is **not**
gated on `email_verified`; the flag on `/auth/me` is for the UI.

### `POST /auth/login` → 200

```json
{ "email": "ada@example.com", "password": "correct horse battery" }
```

Email matching is case-insensitive. An unknown email and a wrong password both
return `401 invalid_credentials` after the same amount of work, so the endpoint
cannot be used to enumerate accounts.

### `POST /auth/refresh` → 200

```json
{ "refresh_token": "opaque" }
```

Body `{ "refresh_token": "opaque" }` **or** the `refresh_token` httpOnly cookie
(`Path=/`, `SameSite=Lax`, `Secure` in production). Login, register and refresh
set the cookie; logout and a failed refresh clear it. JSON still returns a
refresh token for tests and old clients; the SPA prefers the cookie and does
not store refresh in `localStorage`.

Returns a fresh pair and invalidates the one presented: **store the new refresh
token on every call** (or rely on the Set-Cookie rotation). Presenting an already-rotated token is treated as theft —
the whole session is revoked and `401 refresh_token_reused` is returned, so the
user must sign in again.

### `POST /auth/logout` → 204

Revokes the calling session. Idempotent: repeating it with a still-valid access
token succeeds.

### `GET /auth/me` → 200

```json
{
  "user": { "...": "as above" },
  "onboarding": { "quiz_done": false, "preferences_done": false, "profile_done": false, "photos_done": false }
}
```

The flags are computed server-side; use them to route a user into onboarding
rather than inferring readiness from other endpoints.

| Flag | True when |
|------|-----------|
| `quiz_done` | A trait vector exists with all five Big Five keys |
| `preferences_done` | An age range is set and at least one gender is sought |
| `profile_done` | Bio, gender and date of birth are all set — without a birth date the user cannot be age-filtered, so they stay out of everyone's feed |
| `photos_done` | The gallery has at least one entry |

`GET /discover` requires `quiz_done`; the rest are for the UI's progress
indicator. Note `photos_done` is not required to be discoverable.

### `GET /auth/sessions` → 200

```json
{ "items": [ { "id": "uuid", "user_agent": "...", "ip": "203.0.113.4", "current": true,
               "created_at": "...", "last_used_at": "...", "expires_at": "..." } ] }
```

Scoped to the caller. `current` marks the session making the request. Recently
revoked sessions stay listed briefly so the user can see the revocation happened.

### `DELETE /auth/sessions/{id}` → 204

Revokes one session, which is how "sign out my other devices" is built.
Another user's session id is a `404`, not a `403`, so ids cannot be probed.

### `POST /auth/password/forgot` → 202

```json
{ "email": "ada@example.com" }
```

Always 202, whether or not the account exists. Emits
`auth.password_reset_requested`; Person C's mailer delivers the token.

### `POST /auth/password/reset` → 204

```json
{ "token": "opaque", "password": "a new password" }
```

Unknown, expired or already-used tokens are `400 reset_token_invalid`. Tokens
expire after `PASSWORD_RESET_TTL` (default 1h). A successful reset revokes every
session, so all devices must sign in again. A password that fails policy is `422`.

### `POST /auth/email/verify` → 204

```json
{ "token": "opaque" }
```

Marks `email_verified` true. Unknown, expired or already-used tokens are
`400 verify_token_invalid`. Login still works without this step.

### `POST /auth/email/resend` → 202

Authenticated. Issues a new token and retires the previous one. Already-verified
accounts are a silent no-op so the endpoint cannot be used to spam the inbox.

---

## 3. Profile

### `GET /profile` → 200

The caller's own profile, including `email`-free fields plus their own dates:

```json
{
  "user_id": "uuid", "name": "Ada", "bio": "", "gender": "", "location": "",
  "interests": [], "photo_urls": [], "primary_photo_url": "", "photo_url": "",
  "date_of_birth": "1994-05-05", "age": 32,
  "created_at": "...", "updated_at": "..."
}
```

`photo_url` mirrors `primary_photo_url` for the legacy frontend; new code should
read the gallery.

### `PUT /profile` → 200

**Merge semantics**: only the fields present in the body change. `null` and an
absent key both mean "leave alone"; send `""` or `[]` to clear.

```json
{ "bio": "Reader.", "gender": "woman", "location": "Pune",
  "interests": ["coffee", "hiking"], "date_of_birth": "1994-05-05" }
```

- `bio` ≤ 1000 characters, `location` ≤ 120.
- `interests`: ≤ 20 entries, each ≤ 40 characters. Blanks are dropped and
  duplicates removed case-insensitively, keeping the spelling first supplied.
- `gender` is normalised (see `/profile/options`); an unrecognised value is a 422.
- `date_of_birth` updates the user record in the same transaction, so a rejected
  profile change cannot leave a changed birth date behind.

### `PUT /profile/photos` → 200

```json
{ "photo_urls": ["https://cdn.example.test/a.jpg", "/media/b.jpg"] }
```

Replaces the gallery; the first entry becomes `primary_photo_url`. At most 6
entries, each ≤ 1024 characters. Accepted: `https://`, `http://` on loopback
hosts, and same-origin absolute paths (`/media/...`). Rejected: any other scheme
(`javascript:`, `data:`, `vbscript:`), off-origin plaintext HTTP and
protocol-relative (`//host/x.jpg`) URLs, because these end up in an `<img src>`.
Blank entries are dropped.

Sending `asset_ids` instead of `photo_urls` resolves Person C's media assets to
public URLs. Each id must be owned by the caller and `ready`; otherwise the
request is `404 not_found` or `409 asset_not_ready`. HTTPS URLs still work for
seed data and local development.

### `GET /users/{id}/public` → 200

Another user's card. **Never contains `email` or `date_of_birth`** — only `age`:

```json
{ "user_id": "uuid", "name": "Ada", "age": 32, "bio": "", "gender": "woman",
  "location": "Pune", "interests": [], "photo_urls": [], "primary_photo_url": "",
  "is_matched": false }
```

A blocked relationship in either direction is a `404`, so blocking is not
observable from the outside.

### `GET /profile/options` → 200

The canonical gender list and the field limits, so the UI does not hardcode a
list that can drift from the database constraint.

---

## 4. Personality

### `GET /personality/assessment` → 200

```json
{ "version": 1, "scale_min": 1, "scale_max": 5, "can_submit": true, "next_retake_at": null,
  "questions": [ { "id": "uuid", "code": "O1", "prompt": "...", "trait_key": "openness", "direction": 1 } ] }
```

Order is stable across requests, so a partially completed quiz can be resumed.
`direction` is `-1` for reverse-keyed items; the server handles the arithmetic —
send the raw answer.

### `POST /personality/assessment/submit` → 200

```json
{ "answers": [ { "question_id": "uuid", "value": 4 } ] }
```

**Every** question must be answered exactly once, with `value` in 1–5. Partial
submissions, unknown ids, duplicates and out-of-range values are all a 422, and
nothing is stored.

Returns the same body as `/personality/me`. Raw answers are retained so a future
scoring version can be recomputed without asking users to retake the quiz.

### `GET /personality/me` → 200

```json
{ "traits": { "openness": 0.75, "conscientiousness": 0.5, "extraversion": 0.5,
              "agreeableness": 0.25, "neuroticism": 0.5 },
  "version": 1, "assessed_at": "...", "next_retake_at": "...", "can_retake": false }
```

`404` before the first submission — that is the signal to send the user to the
quiz, not an error to report.

Retakes are limited by `ASSESSMENT_RETAKE_INTERVAL` (default 30 days); an early
attempt is `409 retake_too_soon`. A retake replaces the stored vector and
invalidates the cache, so discovery scores update immediately.

---

## 5. Preferences

### `GET /preferences` → 200

```json
{ "age_min": 18, "age_max": 99, "genders": [], "max_distance_km": null,
  "trait_weights": null, "complete": false, "distance_filter_active": false,
  "updated_at": "..." }
```

Registration pre-fills a permissive age range, so `genders` being empty is what
marks preferences incomplete.

`distance_filter_active` is `true` when `max_distance_km` is set. Discovery
applies a Haversine filter (`profiles.lat` / `lng`) when the viewer also has
coordinates. Candidates without coords are excluded while the filter is on.
Set both `lat` and `lng` on `PUT /profile` (or use the SPA “Use my location”
control). `null` `max_distance_km` means anywhere.

### `PUT /preferences` → 200

```json
{ "age_min": 25, "age_max": 40, "genders": ["woman", "nonbinary"],
  "max_distance_km": 50, "trait_weights": { "openness": 2.5 } }
```

**Replace semantics** (unlike `PUT /profile`): this is one settings form, so an
omitted `max_distance_km` or `trait_weights` clears it. `age_min` and `age_max`
are required, within 18–120, and ordered. `genders` needs 1–4 recognised values
and is canonicalised. Trait weights accept the Big Five keys with values in
0–5; unknown keys are a 422.

---

## 6. Discovery & matching

### `GET /discover?limit=20&cursor=...` → 200

```json
{ "items": [ { "user_id": "uuid", "name": "Ada", "age": 32, "bio": "", "gender": "woman",
               "location": "Pune", "interests": [], "photo_urls": [],
               "primary_photo_url": "", "is_matched": false,
               "compatibility_score": 0.87,
               "traits": { "openness": 0.7, "conscientiousness": 0.6,
                           "extraversion": 0.4, "agreeableness": 0.8,
                           "neuroticism": 0.3 } } ],
  "next_cursor": "opaque" }
```

`409 assessment_required` until the caller has taken the assessment.

`traits` is the candidate's stored Big Five vector (values in [0, 1]) so the
discover UI can show top traits on each card. Ranking still uses
`compatibility_score`.

Candidates are excluded when they are the caller, have no assessment, fall
outside the age or gender preferences, are farther than `max_distance_km` from
the viewer (when the viewer has `lat`/`lng` and the preference is set), have already been swiped, or are involved
in a block either way. Scoring and filtering happen in SQL; ordering is
`compatibility_score` descending with the user id as a tiebreaker, which is what
makes the cursor stable.

`limit` defaults to `DISCOVER_DEFAULT_LIMIT` (20) and is capped at
`DISCOVER_MAX_LIMIT` (50); out-of-range or non-numeric values are a 400.

### `POST /likes` → 200

```json
{ "user_id": "uuid", "action": "like" }
```

`action` is `like` or `pass`, case-insensitive.

```json
{ "ok": true, "action": "like", "matched": true, "match_id": "uuid", "duplicate": false }
```

`matched` is true only when the other user had already liked back. Re-sending the
same swipe is safe: it returns `duplicate: true` and the original decision
stands, so a retried request cannot flip a pass into a like. Concurrent mutual
likes produce exactly one match and one `match.created` event.

Errors: `422 cannot_swipe_self`, `422 invalid_action`, `422 validation_failed`
for a missing or nil `user_id`, `404` for an unknown user, `409 blocked_by_you`.

### `GET /matches?limit=20&cursor=...` → 200

```json
{ "items": [ { "match_id": "uuid", "user": { "...": "public profile" },
               "compatibility_score": 0.87, "created_at": "..." } ],
  "next_cursor": null }
```

`compatibility_score` is frozen at match time, so it does not drift when either
user retakes the assessment. It is `null` for matches predating the column.

### `POST /blocks` → 204

```json
{ "user_id": "uuid" }
```

Hides the relationship in both directions and removes the user from discovery.
Idempotent, and reversible with `DELETE`. Emits `user.blocked`.

### `DELETE /blocks/{id}` → 204

Unblocks. Idempotent, and the two users become discoverable to each other again.

### `GET /blocks` → 200

```json
{ "items": [ { "user_id": "uuid", "name": "Ada", "created_at": "..." } ] }
```

The caller's own blocks, capped at 200 — a settings screen, so no cursor.

### `POST /reports` → 202

```json
{ "user_id": "uuid", "reason": "harassment", "details": "optional, ≤1000 chars" }
```

`reason` is one of `spam`, `harassment`, `inappropriate_content`, `fake_profile`,
`underage`, `other`.

```json
{ "id": "uuid", "duplicate": false }
```

A second report against the same user while one is still open returns the
existing id with `duplicate: true` rather than piling up rows.

---

## 7. Events emitted (for Person C)

Written to `outbox_events` in the same transaction as the state change and
published by the drainer, so an event cannot be lost after a commit or emitted
for a rolled-back one. Envelope keys sit alongside the payload:

```json
{ "event_id": "uuid", "event_type": "match.created", "occurred_at": "RFC3339",
  "match_id": "uuid", "user_a_id": "uuid", "user_b_id": "uuid", "created_at": "RFC3339" }
```

| Event | Payload fields |
|-------|----------------|
| `match.created` | `match_id`, `user_a_id`, `user_b_id`, `created_at` |
| `user.blocked` | `blocker_id`, `blocked_id` |
| `auth.password_reset_requested` | `user_id`, `email`, `token`, `expires_at` |

Retries mean an event can be delivered more than once — **consumers must be
idempotent on `event_id`.** `user_a_id` is always the lower uuid of the pair.

`match.created` is the trigger for opening a conversation; use
`matching.Service.IsMatched(ctx, a, b)` to authorise chat rather than querying
the `matches` table directly.

---

## 8. Typical onboarding order

1. `POST /auth/register`
2. `GET /personality/assessment` → `POST /personality/assessment/submit`
3. `PUT /preferences` (genders are what completes it)
4. `PUT /profile` and `PUT /profile/photos`
5. `GET /discover` → `POST /likes` → `GET /matches`

`GET /auth/me` reports progress at every step. Only step 2 gates discovery;
profile and preferences are checked by `onboarding.complete` but not enforced by
the endpoints.

---

## 9. Deprecated pre-v1 endpoints

Still mounted for the current SPA, delegating to the same services, and marked
with `Deprecation: true` and a `Link` header naming the replacement.

| Endpoint | Replacement | Difference |
|----------|-------------|------------|
| `POST /api/auth/register` | `/api/v1/auth/register` | No `date_of_birth`, so accounts start undiscoverable; adds a `token` field aliasing `access_token` |
| `POST /api/auth/login` | `/api/v1/auth/login` | Adds the same `token` field |
| `GET /api/auth/me` | `/api/v1/auth/me` | User fields inlined at the top level instead of under `user` |
| `GET /api/profile` | `/api/v1/profile` | Same body |
| `PUT /api/profile` | `/api/v1/profile` | Replace semantics, and `photo_url` is a single string |
| `GET /api/matches` | `/api/v1/discover` | Offset paging, per-page sorting only, no preference filtering |
| `GET /api/matches/{id}` | `/api/v1/users/{id}/public` | Adds a `score` field |

Tokens are interchangeable across both mounts, so the SPA can migrate one
endpoint at a time. `GET /api/matches` no longer returns other users' email
addresses; that was the leak in the original implementation.

---

## 10. Running it

```bash
cd backend
cp env.example .env          # defaults boot a working development server
go run ./cmd/migrate         # or leave AUTO_MIGRATE=true and let the server do it
go run ./cmd/seed            # optional: fully onboarded demo users
go run ./cmd/server
```

`GET /health` returns `{"status":"ok"}` without authentication.

Tests:

```bash
go test ./...                                  # unit tests
$env:TEST_DATABASE_URL="postgres://postgres:postgres@localhost:5432/dating_test?sslmode=disable"
go test ./internal/integration/...             # end-to-end against real Postgres
```

The integration tests skip themselves unless `TEST_DATABASE_URL` is set, and they
truncate the tables they touch, so point them at a throwaway database.
