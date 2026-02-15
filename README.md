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

### Backend

```bash
cd backend
cp .env.example .env   # set DB URL, JWT secret
go mod download
go run cmd/server/main.go
```

API runs at `http://localhost:8080`.

### Database

Run the SQL in `docs/DATABASE_DESIGN.md` (Section 6) in your PostgreSQL database, or use the migrations in `backend/migrations` (if added).

### Frontend

```bash
cd frontend
npm install
npm run dev
```

App runs at `http://localhost:5173` (or port shown).

## API Overview (PoC)

- `POST /api/auth/register` — Register
- `POST /api/auth/login` — Login (returns JWT)
- `GET /api/auth/me` — Current user (Bearer token)
- `GET /api/profile` — Get own profile
- `PUT /api/profile` — Update profile
- `GET /api/matches` — Get match recommendations
- `GET /api/conversations` — List conversations
- `GET /api/conversations/:id/messages` — Get messages
- `POST /api/conversations/:id/messages` — Send message

## License

Educational / course project.
