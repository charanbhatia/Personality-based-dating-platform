# Personality-Based Dating Platform

A dating platform that matches users based on **personality traits**, **values**, and **behavioral patterns** rather than surface-level attributes.

## Phase 2 — Design & Proof of Concept

This repository contains the **Phase 2** deliverables:

- **System architecture** — [docs/SYSTEM_ARCHITECTURE.md](docs/SYSTEM_ARCHITECTURE.md)
- **Module-wise design** — [docs/MODULE_DESIGN.md](docs/MODULE_DESIGN.md)
- **Technology stack justification** — [docs/TECHNOLOGY_STACK.md](docs/TECHNOLOGY_STACK.md)
- **Database / data flow design** — [docs/DATABASE_DESIGN.md](docs/DATABASE_DESIGN.md)
- **Proof of Concept (PoC)** — Backend (Go) + Frontend (React) + PostgreSQL

## Tech Stack

| Layer    | Technology  |
|----------|-------------|
| Backend  | Go (Golang) |
| Database | PostgreSQL  |
| Frontend | React.js    |
| Auth     | JWT + bcrypt |

## Project Structure

```
bits-assignemnt/
├── docs/                    # Phase 2 documentation
│   ├── SYSTEM_ARCHITECTURE.md
│   ├── MODULE_DESIGN.md
│   ├── TECHNOLOGY_STACK.md
│   └── DATABASE_DESIGN.md
├── backend/                 # Go API server
├── frontend/                # React SPA
└── README.md
```

## Quick Start (PoC)

### Prerequisites

- Go 1.21+
- Node.js 18+
- PostgreSQL 15+

### Database

Create a database and run migrations:

```bash
createdb dating_platform   # or use your PostgreSQL client
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

## API Overview (PoC)

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

Educational / course project.
