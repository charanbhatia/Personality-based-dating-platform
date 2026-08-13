# Team Docs Index — Personality-Based Dating Platform

This root document indexes every planning document created for the startup-style MVP (Persons A, B, and C). Full specs live under [`docs/`](docs/).

## Documents

| Document | Audience | What it covers |
|----------|----------|----------------|
| [docs/00_SHARED_ROADMAP.md](docs/00_SHARED_ROADMAP.md) | Everyone | Feature inventory (F01–F34), ownership matrix, architecture, B↔C event contracts, milestones M1–M4, API conventions (`/api/v1`) |
| [docs/PERSON_A_FRONTEND.md](docs/PERSON_A_FRONTEND.md) | Person A | All React UI flows, API contracts to consume, WebSocket/media UX, frontend scaling (caching, code split, virtualization, a11y) |
| [docs/PERSON_B_BACKEND.md](docs/PERSON_B_BACKEND.md) | Person B | Auth/sessions, profile, personality quiz, preferences, discovery/matching, likes/blocks, B-domain scaling & schemas |
| [docs/PERSON_B_API.md](docs/PERSON_B_API.md) | A and C | The **implemented** B API: request/response shapes, error codes, cursors, emitted events, onboarding order, deprecated pre-v1 paths |
| [docs/PERSON_C_BACKEND.md](docs/PERSON_C_BACKEND.md) | Person C | Messaging, WebSocket, media uploads, notifications, Redis/queues, Docker Compose, CI, observability |

Also see the project overview in [README.md](README.md).

## Ownership (quick)

| Person | Owns | Does not own |
|--------|------|--------------|
| **A** | Frontend UI + FE performance for every feature | Go, DB, Redis, Docker, CI |
| **B** | Auth, profile, personality, preferences, matching | WS gateway, object storage, notification workers, infra |
| **C** | Messaging, realtime, media, notifications + shared infra | Quiz scoring, discover ranking / match creation logic |

## Product loop

Assess personality → set preferences → complete profile/photos → discover & like → mutual match → realtime chat → notifications.

## How to use these docs

1. Read **00_SHARED_ROADMAP** first for the full feature list and milestone order.
2. Each person implements from their **PERSON_*** doc.
3. Contract changes (APIs, events, schemas) update the shared roadmap **before** code.

## Out of scope (MVP)

Payments, video calls, ML re-rankers, multi-region, full trust & safety ops console.
