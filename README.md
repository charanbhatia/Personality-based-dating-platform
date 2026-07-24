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
psql dating_platform -f backend/migrations/001_init.sql
```

### Backend

```bash
cd backend
cp env.example .env       # set DATABASE_URL, JWT_SECRET
go mod download
go run cmd/server/main.go
```

API runs at `http://localhost:8080`.

### Frontend

```bash
cd frontend
npm install
npm run dev
```

App runs at `http://localhost:5173`. Set `VITE_API_URL=http://localhost:8080` if the API is elsewhere.

## API Overview (current PoC)

Legacy paths under `/api/...`. MVP work migrates to `/api/v1/...` (see shared roadmap).

- `POST /api/auth/register` — Register
- `POST /api/auth/login` — Login (returns JWT)
- `GET /api/auth/me` — Current user (Bearer token)
- `GET /api/profile` — Get own profile
- `PUT /api/profile` — Update profile
- `GET /api/matches` — Get match recommendations
- `GET /api/conversations` — List conversations
- `POST /api/conversations` — Start conversation (body: `{"user_id": "uuid"}`)
- `GET /api/conversations/:id/messages` — Get messages
- `POST /api/conversations/:id/messages` — Send message (body: `{"content": "..."}`)

## License

Educational / course project evolving into a startup-style MVP.
