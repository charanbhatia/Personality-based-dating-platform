# Person C — Backend: Messaging, Media, Notifications & Shared Infra

**Role:** Own messaging (REST + WebSocket), media uploads, notifications, and **all shared infrastructure scaling** (Redis, queues, Docker, CI, observability, cross-cutting middleware).  
**Does not own:** Personality quiz scoring, discover ranking, likes/match creation logic (Person B).  
**Collaborates with:** A (WS/media/notif clients), B (JWT claims, match gate, domain events).  
**Source of truth:** [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md).

---

## 1. Purpose

This document is your full playbook: current gaps (including known bugs), feature specs, realtime design, media pipeline, notification workers, and the scaling/runbook layer the whole team depends on.

> **Status (merge branch `feat/b-c-merge`):** M1–M3 below are implemented. The
> table in §2 is the original gap analysis, kept for context. The live env
> surface is [`backend/env.example`](../backend/env.example). B's wire contract
> is [PERSON_B_API.md](./PERSON_B_API.md).

---

## 2. Current baseline (your domain + infra)

| Area | Exists | Gap |
|------|--------|-----|
| Conversations / messages | REST create/list/get/send | Not match-gated; inbox shows raw UUIDs (FE); **bug in `ListByUserID`** |
| Realtime | None | WebSocket + Redis pub/sub required |
| Media | `photo_url` string on profile | No upload, virus/size checks, thumbnails, MinIO/S3 |
| Notifications | None | Queue + inbox API |
| Redis / queues | None | Required for MVP scale story |
| Docker / CI | None | You own Compose + basic CI |
| Observability | None | Structured logs, metrics, traces |
| Middleware | CORS reflect-any Origin | Request ID, rate limit, hardened CORS, ready probe |

**Now shipped (do not treat the table as current):** match-gated `/api/v1/conversations`, WebSocket at `GET /ws` with Redis fanout, media presign + thumbnail worker, notifications inbox + unread counters, Redis Streams queue + `cmd/worker`, Compose (Postgres/Redis/MinIO/Mailhog/api/worker), CI, `/healthz` `/readyz` `/metrics`, request ID + W3C trace context, rate limits. The `ListByUserID` double-bind bug is fixed and covered by `repository/conversation_test.go`.

SQL for C domains is `009_c_messaging.sql`, `010_c_media.sql`, `011_c_notifications.sql` so it runs **after** B's `matches` table (`006_b_matching.sql`). B's `012_b_email_verification.sql` follows C's tables. Do not renumber C's files back to `005_c`–`007_c`.

---

## 3. Owned package layout

```
backend/
  cmd/
    server/          # HTTP + WS (you extend main)
    worker/          # NEW: queue consumers
  internal/
    platform/        # NEW: config, db, redis, queue, middleware, telemetry, storage
    messaging/
    media/
    notifications/
```

B registers domain routes into the mux you expose. You own `cmd/server` wiring order: platform → auth middleware → B routes → C routes → WS.

---

## 4. Platform layer (F23–F27) — foundation for everyone

### 4.1 Config (`platform/config`)

Extend env (maintain `backend/env.example`):

| Variable | Purpose |
|----------|---------|
| `APP_ENV` (alias `ENV`) | development / production; B's production JWT/CORS guards |
| `PORT` | HTTP listen |
| `DATABASE_URL` | Postgres |
| `REDIS_URL` | Redis; empty disables rate limit, queues, WS fanout |
| `JWT_SECRET` | Shared with B (`TokenManager`) |
| `CORS_ALLOWED_ORIGINS` (alias `CORS_ORIGINS`) | Comma-separated allowlist |
| `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_BUCKET`, `S3_PUBLIC_BASE_URL` | Media; empty → media endpoints 503 |
| `WS_ALLOWED_ORIGINS` | WS check |
| `SMTP_*` or `EMAIL_MODE=log` | Email stub |

### 4.2 Health & readiness (F23)

```http
GET /healthz     → 200 {"status":"ok"}           # liveness
GET /readyz      → 200/503                       # Postgres ping + Redis ping
```

Kubernetes/Compose use these; do not require auth.

### 4.3 Middleware (F24)

| Middleware | Behavior |
|------------|----------|
| Request ID | Generate/propagate `X-Request-ID`; put in context + logs |
| Logger | Structured JSON: method, path, status, duration, user_id, request_id |
| CORS | Allowlist from `CORS_ORIGINS` (dev may include Vite origin) |
| Rate limit | Redis token bucket / sliding window; stricter on `/auth/login`, `/auth/register`, `/auth/password/forgot` |
| Auth | Parse Bearer access JWT (claims defined by B); set `user_id` + `sid` on context |
| Recover | Panic → 500 + log stack |

Rate limit keys: `rl:ip:{ip}:{route}` and for auth `rl:email:{hash}:{route}`.

### 4.4 Redis roles

| Use | Key pattern | Owner of logic |
|-----|-------------|----------------|
| Rate limiting | `rl:*` | C platform |
| Pub/sub / streams for chat fanout | `chat:conv:{id}` | C messaging |
| B trait cache | `traits:{user_id}` | B (you provide client) |
| Refresh session secondary index (optional) | `sess:{sid}` | B |
| Notification unread counters | `notif:unread:{user_id}` | C |

Document Redis as **cache/transport**, not system of record (except ephemeral counters that can rebuild from DB).

### 4.5 Queue

Pick one and stick to it for MVP: **Asynq (Redis)** or **Redis Streams + consumer group**.

Queues:

| Queue | Jobs |
|-------|------|
| `email` | password reset, optional match email |
| `media` | thumbnail generation |
| `notifications` | fanout in-app rows from domain events |
| `outbox` (optional) | publish pending B outbox rows |

Worker binary: `go run cmd/worker/main.go`.

### 4.6 Docker Compose (F26)

Services:

- `postgres:15`
- `redis:7`
- `minio` (+ create bucket init)
- `api` (server)
- `worker`
- Optional: `mailhog` for email stub UI

Provide `make up` / `docker compose up` documented in README.

### 4.7 CI (F27)

GitHub Actions (or equivalent):

1. `go test ./...`
2. `go vet` / golangci-lint
3. `frontend` `npm ci && npm run build`
4. Optional: compose integration smoke (`/readyz`)

### 4.8 Observability (F25)

- **Logs:** stdout JSON
- **Metrics:** Prometheus `/metrics` — HTTP latency histograms, WS gauge (connections), queue depth, rate-limit rejections
- **Traces:** OpenTelemetry around handlers + DB (basic)

M3 delivers this; M1 at least structured logs + request IDs.

---

## 5. Messaging domain (F17–F19)

### 5.1 Schema additions

```sql
-- extend conversations
ALTER TABLE conversations
  ADD COLUMN IF NOT EXISTS match_id UUID UNIQUE REFERENCES matches(id),
  ADD COLUMN IF NOT EXISTS last_message_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS last_message_preview TEXT;

-- messages: keep content; add optional client_id for idempotency
ALTER TABLE messages
  ADD COLUMN IF NOT EXISTS client_msg_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS idx_messages_client
  ON messages(conversation_id, sender_id, client_msg_id)
  WHERE client_msg_id IS NOT NULL;

CREATE TABLE conversation_reads (
  conversation_id UUID REFERENCES conversations(id) ON DELETE CASCADE,
  user_id UUID REFERENCES users(id) ON DELETE CASCADE,
  last_read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (conversation_id, user_id)
);
```

Note: `matches` table is **B-owned**. Coordinate migration order: B creates `matches` before you add FK, or add FK in a later migration after B’s M2 lands.

### 5.2 REST API

| Method | Path | Description |
|--------|------|-------------|
| GET | `/api/v1/conversations` | Inbox for current user with peer public profile fields |
| POST | `/api/v1/conversations` | Body `{ "match_id": "..." }` — create or get |
| GET | `/api/v1/conversations/:id/messages` | Cursor pagination (newest page + older cursor) |
| POST | `/api/v1/conversations/:id/messages` | Send `{ "content": "...", "client_msg_id": "..." }` |
| POST | `/api/v1/conversations/:id/read` | Mark read |

#### Gate by mutual match

On create:

1. Load match by id (shared DB / B helper `IsMatched` + participants)
2. Ensure `user_id` is one of the two participants
3. Ensure no block either direction (read `blocks`)
4. Upsert conversation linked to `match_id`

**Reject** legacy `{ "user_id" }` start unless that user is matched — keep temporary shim returning `400` with message to use `match_id`.

#### Inbox DTO

```json
{
  "items": [
    {
      "id": "...",
      "match_id": "...",
      "peer": { "user_id": "...", "name": "...", "photo_url": "..." },
      "last_message_preview": "...",
      "last_message_at": "...",
      "unread_count": 2
    }
  ],
  "next_cursor": null
}
```

Join users/profiles for peer display (read-only on B tables).

#### Fix list query

Correct `ListByUserID` parameter binding; order by `last_message_at DESC NULLS LAST`.

### 5.3 WebSocket gateway (F19)

**Endpoint:** `GET /ws` upgrade.

**Auth options (pick one, document for A):**

1. `Authorization: Bearer` header on handshake (browsers may limit — prefer)
2. `?access_token=` query (works everywhere; slightly leakier in logs — strip from access logs)
3. First message `{ "type":"auth", "token":"..." }` before subscribe

**Protocol (JSON):**

```json
{ "type": "subscribe", "conversation_id": "..." }
{ "type": "unsubscribe", "conversation_id": "..." }
{ "type": "message.send", "conversation_id": "...", "content": "...", "client_msg_id": "..." }
{ "type": "typing.start", "conversation_id": "..." }
{ "type": "typing.stop", "conversation_id": "..." }
{ "type": "message.ack", "client_msg_id": "...", "message": { } }
{ "type": "message.new", "message": { } }
{ "type": "typing", "conversation_id": "...", "user_id": "...", "typing": true }
{ "type": "error", "code": "...", "message": "..." }
{ "type": "ping" }
{ "type": "pong" }
```

**Authorization:** On subscribe/send, verify participant membership.

**Fanout across API replicas:**

```
API-1 receives send → persist message → publish Redis channel chat:conv:{id}
API-1 and API-2 subscribers → push to local WS clients
```

Use Redis **Pub/Sub** or Streams; Pub/Sub is enough for MVP.

**Scaling notes**

- Soft limit connections per instance; metrics gauge `ws_connections`
- Heartbeat every 30s; close dead peers
- Sticky sessions optional if using Pub/Sub (not required when every node subscribes to Redis)

**REST still required** for history and clients that cannot hold WS.

After persist, publish `message.created` to notification queue.

---

## 6. Media domain (F20)

### 6.1 Schema

```sql
CREATE TABLE media_assets (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status TEXT NOT NULL CHECK (status IN ('pending','processing','ready','failed')),
  content_type TEXT NOT NULL,
  byte_size INT,
  original_key TEXT NOT NULL,
  thumb_key TEXT,
  original_url TEXT,
  thumb_url TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_media_user ON media_assets(user_id);
```

### 6.2 API

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/v1/media/presign` | `{ "content_type": "image/jpeg", "byte_size": 123 }` → upload URL |
| GET | `/api/v1/media/assets/:id` | Status + URLs when ready |
| DELETE | `/api/v1/media/assets/:id` | Owner only |

**Presign rules**

- Allow `image/jpeg`, `image/png`, `image/webp`
- Max size e.g. 10MB
- Key: `users/{user_id}/{asset_id}/original`

**Worker**

1. Job `media.process` with `asset_id`
2. Download original from bucket
3. Generate max-edge thumbnail (e.g. 512px)
4. Upload thumb; set URLs; `status=ready`
5. Publish `media.processed` (`user_id`, `asset_id`, urls) so B/A can attach to profile

Local dev: MinIO. Production: any S3-compatible store.

---

## 7. Notifications domain (F21–F22)

### 7.1 Schema

```sql
CREATE TABLE notifications (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  type TEXT NOT NULL, -- match.created | message.created | system
  title TEXT NOT NULL,
  body TEXT NOT NULL,
  data JSONB NOT NULL DEFAULT '{}',
  read_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_notifications_user_created ON notifications(user_id, created_at DESC);
```

### 7.2 API

| Method | Path |
|--------|------|
| GET | `/api/v1/notifications` |
| GET | `/api/v1/notifications/unread-count` |
| POST | `/api/v1/notifications/:id/read` |
| POST | `/api/v1/notifications/read-all` |

### 7.3 Workers

| Event in | Action |
|----------|--------|
| `match.created` | Insert notification for **both** users; bump Redis unread; optional email |
| `message.created` | Notify **recipient** only; skip if active WS on that conversation (optimization M4) |
| `auth.password_reset_requested` | Send email (or log in `EMAIL_MODE=log`) |
| `auth.email_verification_requested` | Send confirmation email (or log in `EMAIL_MODE=log`) |

Push notifications (FCM/APNs): real pipeline. `POST /api/v1/devices` `{ token, platform: web|android|ios }` upserts a token; `DELETE` removes it. When `FCM_SERVER_KEY` and/or `APNS_*` are set, match/message events push to stored tokens. With no tokens the worker still logs `would push`.

---

## 8. Shared infra scaling playbook

This is the core of “startup-style scaling” for the team.

### 8.1 Horizontal API

- Stateless API containers behind a load balancer
- JWT validation local (shared secret); no sticky required for REST
- WS: Redis pub/sub so any node can deliver

### 8.2 Postgres

- Tune `pgxpool`: max conns ≈ `(API replicas * per_proc_pool)` under Postgres `max_connections`
- Indexes on messaging: `(conversation_id, created_at DESC)` for messages
- Consider partitioning `messages` by time **only if** M4 load tests show need (document; do not preemptively partition)
- Migrations: single ordered chain; coordinate with B on number allocation

### 8.3 Redis

- One Redis for MVP; separate logical DB indexes or key prefixes
- Memory policy: `allkeys-lru` for cache keys; **do not** LRU rate-limit or session keys without understanding impact — use TTLs explicitly
- Monitor evictions

### 8.4 Workers

- Concurrency limits per queue
- Idempotent handlers keyed by `event_id`
- Dead-letter queue / retry with backoff
- Separate worker deploy from API so chat spikes do not starve emails

### 8.5 Backpressure

- Rate limit message send (e.g. 5/s/user)
- Max WS message size / content length (e.g. 4000 chars)
- Media upload rate limit per user/day

### 8.6 Load-test targets (M4)

| Scenario | Target |
|----------|--------|
| REST send message | p95 &lt; 100ms at 100 RPS |
| WS fanout 2 nodes | message visible on peer &lt; 200ms local |
| `/readyz` under load | remains 200 if deps healthy |
| Worker email queue | drain password-reset &lt; 5s lag |

Publish a short `docs/RUNBOOK_LOADTEST.md` in M4 (you author).

### 8.7 Security (platform)

- Secrets only via env
- CORS allowlist (stop reflect-any in production)
- Strip tokens from logs
- TLS termination at reverse proxy (document Compose override for prod)
- Bucket private; only presigned PUT + public read on CDN/public prefix for thumbs

---

## 9. Contracts with B

| You need from B | When |
|-----------------|------|
| `matches` table + stable IDs | Before conversation FK / gate (M2) |
| `IsMatched` / participant helper | M2 |
| JWT access claim fields | M1 |
| `match.created`, `user.blocked`, password-reset events | M2–M3 |
| Public profile fields readable for inbox peer | M2 |

| B needs from you | When |
|------------------|------|
| Redis client + rate limit | M1 |
| Queue publisher API | M1–M2 |
| Media URLs / `media.processed` | M2 |
| Compose Postgres reachable | M1 |

**Do not** reimplement discover scoring or swipe tables.

---

## 10. Testing plan (C)

| Area | Tests |
|------|-------|
| Conversation list | Regression for single-arg query bug |
| Match gate | Cannot create convo without match; cannot read others’ messages |
| WS | Auth failure; subscribe ACL; fanout with miniredis |
| Media | Reject bad content-type; worker marks ready |
| Notifications | match event → two rows; mark read |
| Platform | `/readyz` fails when Redis down; rate limit trips |

---

## 11. Milestone checklist (C)

### Spec coverage (required vs optional)

| Spec item | Status |
|-----------|--------|
| F23 `/healthz` `/readyz` `/metrics` | Required — done. Empty `REDIS_URL`: ready if Postgres is up, Redis reported `disabled` |
| F24 request ID, structured logs, CORS allowlist, recover | Required — done. Production refuses empty CORS **and** `*` |
| F24 stricter auth rate limits | Required — done on **v1 and legacy** register/login/forgot/refresh/verify. No-op without Redis |
| F25 Prometheus metrics + W3C `traceparent` | Required M3 — done. Full OTLP exporter is **M4** |
| F26 Compose (Postgres, Redis, MinIO, Mailhog, api, worker) | Required — done (`make up`). README still emphasises local `go run` |
| F27 CI `go test` + `go vet` + frontend build | Required — done. golangci-lint and compose smoke are **optional** and not in CI |
| F17–F18 match-gated REST, inbox peer DTO, cursor, `client_msg_id`, `/read` | Required — done |
| `user.blocked` hide thread | Required — **synchronous**: send/subscribe 403; inbox omits blocked peers. No extra queue consumer (monolith reads `blocks`) |
| F19 WS `/ws` Bearer + `?access_token=`, Redis fanout, ping/pong | Required — done |
| Skip notify if recipient has an active WS | **M4 optimization** — not built; every message still inserts an inbox row |
| F20 presign + complete + worker thumb + `media.processed` | Required — done. Keys use `public/users/...` so thumbs are anonymously readable. 503 without S3 |
| B attaching photos | Required — B `PUT /profile/photos` resolves `asset_ids`; does not subscribe to `media.processed` |
| F21 notifications API + match/message consumers + unread Redis | Required — done; idempotent on `event_id` |
| F22 email stub | Required — done (`EMAIL_MODE=log` or SMTP) including email verification |
| F22 push (FCM/APNs) | Device tokens + FCM legacy HTTP / APNs p8 when env is set; otherwise log `would push` |
| Unknown queue events | Ack, do not retry |
| Legacy `/api/conversations` | Deprecated shim — **not** match-gated; v1 is the contract |
| M4 two-replica demo doc, load numbers, ops runbooks | **Deferred M4** |

Error handling that is intentional: match missing → 404; not a participant → 403; legacy `{user_id}` while the gate is on → 400; media wrong owner → 403; unknown media id → 404; storage down → 503; rate limit → 429; Redis down on **auth** limits → 503 (fail-closed); Redis down on general API limits → fail-open.

### M1

- [x] `platform/` package: config, redis, middleware, logging, queue, metrics
- [x] `/healthz`, `/readyz` (and `/health` kept for the current SPA)
- [x] Docker Compose: Postgres, Redis, MinIO, Mailhog, api, worker
- [x] Rate limit on auth routes (Redis; no-op without `REDIS_URL`; fail-closed on auth; v1 **and** legacy)
- [x] CORS allowlist (`CORS_ALLOWED_ORIGINS` / `CORS_ORIGINS`)
- [x] `env.example` updated — merged with B's auth/outbox knobs
- [x] Fix conversation `ListByUserID` bug + test

### M2

- [x] Media presign + worker thumbnails + asset GET (`010_c_media.sql`)
- [x] Conversations match-gated + inbox DTOs (`009_c_messaging.sql`)
- [x] Messages cursor pagination + idempotent `client_msg_id`
- [x] CI pipeline (`.github/workflows/ci.yml`)
- [x] Queue skeleton + worker binary (`cmd/worker`)

### M3

- [x] WebSocket gateway + Redis fanout (`GET /ws`)
- [x] Notifications API + consumers for match/message (`011_c_notifications.sql`)
- [x] Email stub for password reset **and** email verification (`EMAIL_MODE=log` or SMTP/Mailhog)
- [x] Prometheus `/metrics` + W3C `traceparent` on requests (full OTLP exporter still M4)
- [x] Unread counters
- [x] Push: `POST/DELETE /api/v1/devices`; FCM/APNs when configured, else log `would push`

### M4

- [ ] Two-replica WS demo documented
- [ ] Load-test numbers published
- [ ] Runbook: restart worker, rotate JWT secret procedure, disk/redis failure
- [ ] DLQ / retry verified under worker restart (outbox → Redis Streams is wired; not load-tested)

---

## 12. Non-ownership (do not build)

- Personality questions / trait math
- Discover ranking / swipe / match insertion (consume events only)
- Preferences / profile field validation beyond reading for inbox
- React UI (A) — provide clear WS and media contracts only

---

## 13. Risks

| Risk | Mitigation |
|------|------------|
| WS auth awkward in browsers | Support query token + document; prefer subprotocol if feasible |
| Match table not ready | Feature-flag gate; stub match check in dev only behind env |
| MinIO vs real S3 differences | Use AWS SDK S3 API compatibility; test both endpoints |
| Redis single point of failure | Document restart; API degrades: rate limit fail-open vs fail-closed — **fail-closed on auth routes**, fail-open with warning on discover cache |
| Chat without match (legacy clients) | Hard cutover to `match_id` with clear errors |

---

## 14. Definition of Done (C)

1. Platform services run via Compose; `/readyz` green.
2. Messaging secure (participant ACL + match gate).
3. WS delivers messages across 2 processes with Redis.
4. Media upload produces thumb URLs usable by B/A.
5. Notifications appear for match and message events.
6. CI runs tests/lint/build.
7. Metrics endpoint scrapable; request IDs in logs.
8. Shared roadmap events consumed/produced as specified.
9. Milestone checklist complete.

---

## 15. References

- Shared roadmap: [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md)
- B domain: [PERSON_B_BACKEND.md](./PERSON_B_BACKEND.md)
- B API (implemented): [PERSON_B_API.md](./PERSON_B_API.md)
- A consumer: [PERSON_A_FRONTEND.md](./PERSON_A_FRONTEND.md)
- Env: [`backend/env.example`](../backend/env.example)
- Worker: `backend/cmd/worker/main.go`
- Router: `backend/internal/router/router.go`
