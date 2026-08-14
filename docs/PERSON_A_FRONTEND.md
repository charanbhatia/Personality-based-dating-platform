# Person A — Frontend Ownership & Scaling

**Role:** Own **all** React UI and frontend performance/scaling for every MVP feature.  
**Does not own:** Go services, database, Redis, queues, Docker, CI.  
**Collaborates with:** B (auth, profile, personality, matching APIs), C (messaging, WebSocket, media, notifications APIs).  
**Source of truth for product order:** [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md).

---

## 1. Purpose of this document

This is your implementation playbook. It maps:

- What exists today in `frontend/`
- Every screen and flow you must ship
- Exact API contracts you consume from B and C
- How to scale the SPA (performance, caching, realtime, media)
- Milestone checklists and Definition of Done

When B or C change a contract, they update the shared roadmap first; you update this doc’s contract section and the client.

---

## 2. Current frontend baseline

### Stack

| Piece | Today |
|-------|--------|
| Framework | React 19 + Vite 7 |
| Routing | `react-router-dom` 7 |
| HTTP | Axios (`src/api.js`) |
| Auth state | `AuthContext` + `localStorage` (`token`, `user`) |
| Styling | `App.css` / `index.css` (PoC dark UI) |

### Routes (`src/App.jsx`)

| Path | Page | Status |
|------|------|--------|
| `/login`, `/register` | Auth forms | Working PoC |
| `/` | Dashboard links | Working PoC |
| `/profile` | Edit bio/gender/location/photo URL | Working PoC |
| `/matches`, `/matches/:id` | Discovery list/detail | Working PoC |
| `/conversations` | Inbox | Working PoC (shows UUIDs) |
| `/conversations/start/:userId` | Creates convo | Working PoC |
| `/conversations/:id` | Chat | Working PoC (no realtime) |

### Gaps (your backlog)

- No refresh-token handling; 401 always nukes session
- No onboarding quiz or preferences UI
- No like/pass / mutual-match UX
- Chat has no WebSocket; messages load once
- Photo is a free-text URL, not upload
- No notifications UI
- No data library (React Query/SWR), no code splitting, no virtualization
- API paths are legacy `/api/...` — migrate to `/api/v1/...` with B/C

---

## 3. Target UX information architecture

```
/login | /register | /forgot-password | /reset-password
/onboarding
  /onboarding/quiz
  /onboarding/preferences
  /onboarding/profile
  /onboarding/photos
/app                          # authenticated shell
  /app                        # home / next actions
  /app/discover               # swipe or card feed
  /app/matches                # mutual matches
  /app/matches/:id            # match profile
  /app/messages               # conversation list
  /app/messages/:id           # chat thread
  /app/notifications
  /app/profile                # own profile edit
  /app/settings               # sessions, logout, block list
```

Keep old routes as redirects during migration so demos do not break.

**Onboarding gate:** If `GET /api/v1/auth/me` returns `onboarding: { quiz_done, preferences_done, profile_done, photos_done }` incomplete, redirect into `/onboarding/*` before discovery.

---

## 4. Feature specs (frontend)

### 4.1 Auth (F28) — consumes B F01–F04

**Screens**

- Login: email, password, link to register / forgot password
- Register: name, email, password, optional DOB
- Forgot / reset password flows
- Settings → active sessions → revoke

**Client behavior**

1. Store `access_token` in memory. Refresh lives in an `httpOnly` `refresh_token` cookie (`Path=/`, `SameSite=Lax`, `Secure` in production). Axios uses `withCredentials: true`. A leftover `localStorage` refresh token is still sent on refresh as a fallback.
2. Axios (or fetch wrapper) attaches access token.
3. On `401`, attempt **one** refresh via `POST /api/v1/auth/refresh` (empty body + cookie); retry original request; if refresh fails, clear session → `/login`.
4. Logout calls `POST /api/v1/auth/logout` then clears client state (and unregisters the web device token).

**States:** loading, validation errors, network error, success redirect to onboarding or `/app`.

**Acceptance**

- [x] Expired access token silently refreshes without kicking user mid-form
- [x] Logout invalidates refresh (no silent re-login)
- [x] Protected routes use a single `ProtectedRoute` + onboarding gate

---

### 4.2 Onboarding — quiz, preferences, profile, photos (F29, F32)

#### Quiz (B F07)

- Fetch questions: `GET /api/v1/personality/assessment`
- Submit answers: `POST /api/v1/personality/assessment/submit`
- Show trait results (radar or bars): openness, conscientiousness, extraversion, agreeableness, neuroticism
- Allow continue even if retake is locked (show next retake date from API)

#### Preferences (B F09)

- Form: age min/max, genders sought, max distance (km), optional trait weight sliders
- `PUT /api/v1/preferences`

#### Profile (B F05)

- Bio, gender, location, interests (chips), DOB if not set at register
- `PUT /api/v1/profile`

#### Photos (C F20 + B F06)

1. `POST /api/v1/media/presign` → `{ upload_url, asset_id, headers }`
2. `PUT` file directly to object storage with progress bar
3. Poll `GET /api/v1/media/assets/:id` until `status=ready` **or** listen for notification
4. `PUT /api/v1/profile/photos` with ordered `asset_id`s (B)

**Acceptance**

- [x] User cannot reach Discover until quiz + preferences + profile minimum fields complete (photos can be soft-required: at least one)
- [x] Upload shows progress and failure retry
- [x] Trait results render from server scores, not client-invented math

---

### 4.3 Discovery & matching (F30) — consumes B F10–F13, F14–F15

**Discover feed**

- Infinite scroll / “Next” card UI calling `GET /api/v1/discover?limit=&cursor=`
- Card shows: name, age, location, primary photo, compatibility %, short bio, top traits
- Actions: Like, Pass (and optional Super-like later — out of scope)
- Empty state when `items` empty and no `next_cursor`
- Optimistic UI: swipe removes card; rollback on API error

**Matches**

- `GET /api/v1/matches` — mutual matches list
- Detail: `GET /api/v1/users/:id/public` or match detail endpoint from B
- CTA: “Message” → navigates to messages (C creates/gets conversation by `match_id`)

**Block / report**

- From profile menu: Block (`POST /api/v1/blocks`), Report (`POST /api/v1/reports`)
- Confirm modal; on success remove from feed and matches locally

**Acceptance**

- [x] Feed is ordered by server score (do not re-sort client-side except sticky UX)
- [x] Like/pass are idempotent from UI (disable double-tap)
- [x] Mutual match celebration modal when like response `matched: true`
- [x] No email displayed for other users

---

### 4.4 Messaging (F31) — consumes C F17–F19

**Inbox**

- `GET /api/v1/conversations` — show other participant name + avatar + last message preview + unread badge
- Empty state: “Match with someone to start chatting”

**Thread**

1. Load history: `GET /api/v1/conversations/:id/messages?cursor=&limit=`
2. Connect WebSocket: `wss://.../ws?token=...` or `Sec-WebSocket-Protocol` (follow C’s final auth scheme)
3. Optimistic send: append local message → confirm on `message.ack` / REST fallback
4. Typing indicators if C exposes them (optional M3); otherwise skip
5. On WS disconnect: exponential backoff reconnect; fall back to 10s polling while offline

**Start chat**

- From match: `POST /api/v1/conversations { "match_id": "..." }` → navigate to thread
- Do **not** allow starting chat by raw `user_id` unless they are matched (server enforces)

**Acceptance**

- [x] New messages appear without full page refresh when WS connected
- [x] History pagination upward scroll works
- [x] Unmatched conversation attempt shows clear error from API

---

### 4.5 Notifications (F33) — consumes C F21

- Bell in app shell with unread count: `GET /api/v1/notifications/unread-count`
- Inbox list: `GET /api/v1/notifications`
- Mark read: `POST /api/v1/notifications/:id/read` or mark-all
- Click → deep link to match or conversation
- Optional: request browser Notification permission; show when tab hidden. Register `POST /api/v1/devices` so C can deliver FCM/APNs when keys are configured.

**Acceptance**

- [x] Match and message events produce inbox rows within a few seconds of backend processing
- [x] Unread badge stays consistent after mark-read

---

### 4.6 Settings & polish

- Logout, session list
- Edit profile / photos / preferences / retake quiz (if allowed)
- Blocked users list
- Error boundary page for unexpected crashes
- Toast system for API errors (`error.message`)

---

## 5. API client contracts (consume-only)

Centralize in `src/api/` modules (TypeScript): `auth.ts`, `profile.ts`, `personality.ts`, `preferences.ts`, `discover.ts`, `matches.ts`, `conversations.ts`, `media.ts`, `notifications.ts`.

### Auth (B)

```http
POST /api/v1/auth/register
POST /api/v1/auth/login
POST /api/v1/auth/refresh
POST /api/v1/auth/logout
GET  /api/v1/auth/me
POST /api/v1/auth/password/forgot
POST /api/v1/auth/password/reset
GET  /api/v1/auth/sessions
DELETE /api/v1/auth/sessions/:id
```

Login response shape (expected):

```json
{
  "user": { "id": "...", "email": "...", "name": "..." },
  "access_token": "...",
  "refresh_token": "...",
  "expires_in": 900
}
```

### Profile / personality / preferences (B)

```http
GET/PUT /api/v1/profile
PUT     /api/v1/profile/photos
GET     /api/v1/personality/assessment
POST    /api/v1/personality/assessment/submit
GET     /api/v1/personality/me
GET/PUT /api/v1/preferences
```

### Discover / social graph (B)

```http
GET  /api/v1/discover?limit=20&cursor=
POST /api/v1/likes          { "user_id": "...", "action": "like"|"pass" }
GET  /api/v1/matches
POST /api/v1/blocks         { "user_id": "..." }
POST /api/v1/reports        { "user_id": "...", "reason": "..." }
```

Discover item (expected):

```json
{
  "user_id": "...",
  "name": "...",
  "age": 28,
  "bio": "...",
  "gender": "...",
  "location": "...",
  "photo_urls": ["..."],
  "score": 0.87,
  "traits": { "openness": 0.7, "extraversion": 0.4 }
}
```

### Messaging / media / notifications (C)

```http
GET/POST /api/v1/conversations
GET/POST /api/v1/conversations/:id/messages
POST     /api/v1/media/presign
GET      /api/v1/media/assets/:id
GET      /api/v1/notifications
GET      /api/v1/notifications/unread-count
POST     /api/v1/notifications/:id/read
WS       /ws
```

If a field is missing in early milestones, mock with MSW using the shapes above so UI work continues.

---

## 6. Frontend scaling & performance

These are **your** scaling responsibilities (F34). Backend scale is B/C.

### 6.1 Data layer

- Introduce **TanStack Query** (or SWR) for server state: discover, matches, conversations, notifications.
- Cache keys: `['discover', cursor]`, `['conversation', id]`, `['me']`, etc.
- Invalidate on mutations: like → invalidate discover; send message → invalidate conversation list.
- Deduplicate in-flight requests; set stale times (discover 30s, profile 60s, messages 0 with WS).

### 6.2 Code splitting

- Lazy-load route modules: `React.lazy` + `Suspense` for onboarding, discover, chat, settings.
- Keep auth shell and layout eager.
- Budget: initial JS gzip **&lt; 200KB** for first meaningful paint of login/home.

### 6.3 Lists & images

- Virtualize long lists (conversations, notifications, discover grid) with `@tanstack/react-virtual` or equivalent.
- Always use thumbnail URLs from media API for lists; full size only in lightbox.
- `loading="lazy"` + explicit width/height to reduce CLS.
- Prefer CDN/public URL from C; never blob-hoard large files in memory after upload.

### 6.4 Realtime resilience

- Single shared `WebSocketClient` module (connection manager): connect once per session, multiplex by conversation subscription messages.
- Heartbeat / ping if C requires; reconnect with jittered exponential backoff (cap 30s).
- Online/offline banner in shell.

### 6.5 Auth & security on the client

- Do not log tokens.
- Sanitize any rich text (bios are plain text — render as text nodes).
- CSP-friendly: no inline scripts; configure Vite accordingly where possible.
- Depend on server for authorization; UI hiding is not security.

### 6.6 UX performance

- Use `startTransition` for non-urgent feed updates when appropriate.
- Avoid unnecessary `useMemo`/`useCallback` unless measured or required by deps; follow React 19 patterns already in the ecosystem.
- Skeleton loaders instead of blank screens.
- Error boundaries per major route section.

### 6.7 Tooling

- Env: `VITE_API_URL`, `VITE_WS_URL`
- ESLint + type checking path: prefer migrating critical modules to TypeScript gradually (`api/` and `types/`) — optional but recommended in M3–M4
- Lighthouse CI optional in M4 (coordinate with C’s CI)

### 6.8 Accessibility

- Focus management on route changes
- Buttons/links keyboard operable
- ARIA labels on icon-only controls (like/pass, bell)
- Sufficient contrast on final visual system (coordinate design; do not block on perfect brand)

---

## 7. Suggested folder structure

```
frontend/src/
  api/                 # HTTP + WS clients
  auth/                # AuthProvider, token refresh
  components/          # shared UI
  features/
    onboarding/
    discover/
    matches/
    chat/
    notifications/
    profile/
  hooks/
  pages/               # thin route wrappers
  styles/
  App.jsx
  main.jsx
```

Migrate incrementally; do not big-bang rewrite in M1.

---

## 8. Milestone checklist (A)

### M1

- [x] API client supports `/api/v1` + refresh interceptor
- [x] Login / register / logout / me
- [x] Onboarding shell: quiz + preferences + profile forms wired to B
- [x] Basic Query provider + error toasts
- [x] Route gate for onboarding flags

### M2

- [x] Discover feed + like/pass + match celebration
- [x] Matches list + open chat via `match_id`
- [x] Photo upload with progress
- [x] Chat REST history + polling fallback
- [x] Redirects from legacy routes

### M3

- [x] WebSocket chat integration
- [x] Notifications bell + inbox
- [x] Block / report flows
- [x] Session management UI

### M4

- [x] Virtualized lists, image discipline, bundle budget met
- [x] Error boundaries + empty/error/loading consistency
- [x] A11y pass on primary flows
- [x] Remove legacy API paths from client

---

## 9. Collaboration rules

| Situation | Action |
|-----------|--------|
| Need a field not in API | File request with feature ID; use MSW stub meantime |
| B/C breaking change | They update roadmap + notify; you bump client in same sprint |
| Design ambiguity | Prefer one job per screen (see product UI rules); ask team before adding dashboard clutter |
| Perf regression | Profile with React DevTools / Lighthouse; fix in A before asking B/C to “make API faster” unless network-bound |

**You do not:** change Go handlers, write migrations, configure Redis, or “temporarily” hardcode match scores in the UI.

---

## 10. Risks & mitigations

| Risk | Mitigation |
|------|------------|
| B/C APIs late | MSW fixtures from §5 shapes |
| Token storage XSS | Prefer httpOnly refresh cookie when available; minimize access token lifetime |
| WS flaky on mobile networks | Polling fallback + backoff |
| Scope creep (video, payments UI) | Reject; point to roadmap out-of-scope |
| Rewrite itch | Incremental feature folders; ship vertical slices |

---

## 11. Definition of Done (A)

A frontend feature is done when:

1. Happy path works against real `/api/v1` (or documented stub for the milestone).
2. Loading, empty, and error states are implemented.
3. Auth/onboarding gates behave correctly.
4. No PII of other users (email) shown.
5. Query cache invalidation is correct for the mutation.
6. Mobile-width usable (responsive) for that flow.
7. Checklist item in §8 marked complete for the milestone.

---

## 12. References

- Current entry: `frontend/src/App.jsx`, `frontend/src/api.js`, `frontend/src/context/AuthContext.jsx`
- Shared roadmap: [00_SHARED_ROADMAP.md](./00_SHARED_ROADMAP.md)
- Backend contracts: [PERSON_B_BACKEND.md](./PERSON_B_BACKEND.md), [PERSON_C_BACKEND.md](./PERSON_C_BACKEND.md)
