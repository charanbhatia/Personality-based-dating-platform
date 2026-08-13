# Personality-Based Dating Platform

A dating platform that matches users based on **personality traits**, **values**, and **behavioral patterns** rather than surface-level attributes.

## Phase — Startup-style MVP

Production-minded APIs with real scaling foundations: Redis, job queues, realtime chat, and observability. Work is split across three owners.

## Team ownership

| Person | Scope | Document |
|--------|--------|----------|
| **A** | All frontend UI + frontend performance/scaling for every feature | [docs/PERSON_A_FRONTEND.md](docs/PERSON_A_FRONTEND.md) |
| **B** | Auth, profile, personality assessment, preferences, matching | [docs/PERSON_B_BACKEND.md](docs/PERSON_B_BACKEND.md) |
| **C** | Messaging, realtime, media, notifications + shared infra scaling | [docs/PERSON_C_BACKEND.md](docs/PERSON_C_BACKEND.md) |

Doc index (all planning docs in one place):

- [TEAM_DOCS.md](TEAM_DOCS.md)

Shared product roadmap, ownership matrix, milestones, and B↔C event contracts:

- [docs/00_SHARED_ROADMAP.md](docs/00_SHARED_ROADMAP.md)

## Tech Stack

| Layer | Technology |
|-------|------------|
| Backend | Go (Golang) modular monolith |
| Database | PostgreSQL |
| Cache / pub-sub | Redis |
| Jobs | Queue workers (e.g. Redis streams / Asynq) |
| Frontend | React.js (Vite) |
| Auth | JWT (access + refresh) + bcrypt |
| Realtime | WebSocket gateway |
| Media | S3-compatible object storage |

## Project Structure

```
Personality-based-dating-platform/
├── docs/                      # Team ownership & roadmap docs
│   ├── 00_SHARED_ROADMAP.md
│   ├── PERSON_A_FRONTEND.md
│   ├── PERSON_B_BACKEND.md
│   ├── PERSON_B_API.md        # Implemented auth/profile/personality/matching API
│   └── PERSON_C_BACKEND.md
├── backend/                   # Go API server
├── frontend/                  # React SPA
└── README.md
```

## Quick Start (current PoC)

### Prerequisites

- Go 1.21+
- Node.js 18+
- PostgreSQL 15+

### Database

```bash
createdb dating_platform
```

Schema changes are applied by a migration runner, not by loading a single file:
the server applies anything pending at boot, or run it as a separate step.

```bash
cd backend
go run ./cmd/migrate            # apply pending migrations
go run ./cmd/migrate -status    # show what is applied
```

### Backend

```bash
cd backend
cp env.example .env       # defaults boot a working development server
go mod download
go run ./cmd/server
```

API runs at `http://localhost:8080`; `GET /health` needs no authentication.
Every setting is documented in [backend/env.example](backend/env.example).

Optional demo data — fully onboarded users with traits, preferences and photos,
so the discovery feed is not empty:

```bash
go run ./cmd/seed
```

### Frontend

```bash
cd frontend
npm install
npm run dev
```

App runs at `http://localhost:5173`. Set `VITE_API_URL=http://localhost:8080` if the API is elsewhere.

## API Overview

Current endpoints live under `/api/v1/...`. Full request and response shapes,
error codes and the emitted event contracts are in
[docs/PERSON_B_API.md](docs/PERSON_B_API.md).

| Group | Endpoints |
|-------|-----------|
| Auth | `POST /auth/register`, `/auth/login`, `/auth/refresh`, `/auth/logout`, `/auth/password/forgot`, `/auth/password/reset`; `GET /auth/me`, `/auth/sessions`; `DELETE /auth/sessions/{id}` |
| Profile | `GET`/`PUT /profile`, `PUT /profile/photos`, `GET /profile/options`, `GET /users/{id}/public` |
| Personality | `GET /personality/assessment`, `POST /personality/assessment/submit`, `GET /personality/me` |
| Preferences | `GET`/`PUT /preferences` |
| Discovery | `GET /discover`, `POST /likes`, `GET /matches` |
| Safety | `GET`/`POST /blocks`, `DELETE /blocks/{id}`, `POST /reports` |

The pre-v1 paths (`/api/auth/*`, `/api/profile`, `/api/matches`) still work for
the current SPA and carry a `Deprecation` header naming their replacement.
Messaging endpoints (`/api/conversations/...`) are Person C's.

## License

Educational / course project evolving into a startup-style MVP.
