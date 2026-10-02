# CampusBite — High-Concurrency Campus Food Ordering Platform

CampusBite is a high-performance, real-time cafeteria ordering and queue management system designed for university and college campuses. Built as a clean **Modular Monolith** with Go, PostgreSQL, Redis, WebSockets, and React, it ensures zero race conditions, sub-second live order tracking, transactional inventory reservation, and conversational business analytics.

---

## Production & Development Architecture

```
                                  ┌───────────────────────────┐
                                  │      Client Browsers      │
                                  └─────────────┬─────────────┘
                                                │
                       ┌────────────────────────┴────────────────────────┐
                       │                                                 │
            HTTP/REST Requests                                 Real-Time WebSockets
                       │                                                 │
                       ▼                                                 ▼
        ┌─────────────────────────────┐                   ┌─────────────────────────────┐
        │       React Frontend        │                   │    WebSocket Hub (Go)       │
        │    (Vercel Production)      │                   │    (Live Status Broadcast)  │
        └──────────────┬──────────────┘                   └──────────────┬──────────────┘
                       │                                                 │
                       │              Go + Gin Modular Monolith          │
                       └────────────────────────►◄───────────────────────┘
                                                │
                                    ┌───────────┴───────────┐
                                    │                       │
                                    ▼                       ▼
                       ┌─────────────────────────┐  ┌─────────────────────────┐
                       │  PostgreSQL Database    │  │       Redis 8           │
                       │ (Authoritative Truth)   │  │ (Cache-Aside & Rate Lim)│
                       │ - Row Locks (FOR UPDATE)│  │ - 60s Menu Catalog Cache│
                       │ - 15-min Reservations   │  │ - Atomic Lua Limiters   │
                       │ - Idempotency Keys      │  │ - Fail-Open Resilience  │
                       │ - Durable Notifications │  └─────────────────────────┘
                       └─────────────────────────┘
                                    │
                                    ├───► Razorpay (Sandbox / Webhook Verification)
                                    └───► Gemini API (Structured Intent Classification)
```

---

## Tech Stack

| Layer | Technology | Role in Architecture |
| :--- | :--- | :--- |
| **Backend** | **Go 1.26+ / Gin** | Core API, transactional checkout, WebSocket hub, background maintenance workers |
| **Frontend** | **React 19 / JavaScript / Vite / Tailwind CSS** | Single-page UI, live preparation stepper, tray management, admin dashboard |
| **Primary Database** | **PostgreSQL 18 (`pgx/pgxpool`)** | **Authoritative source of truth** for catalog, inventory, reservations, orders, payments, and notifications |
| **Cache & Limiter** | **Redis 8** | High-speed cache-aside for public menu reads, atomic Lua rate limiting |
| **Realtime** | **Gorilla WebSocket (Native Go)** | Live kitchen status updates (`ORDER_STATUS_UPDATED`), admin order alerts (`NEW_ORDER`) |
| **Payments** | **Razorpay API** | Payment initiation, HMAC-SHA256 signature verification, idempotent asynchronous webhooks |
| **AI Intelligence** | **Google Gemini API** | Natural-language query intent routing & grounded demand trend explanations (zero text-to-SQL) |
| **Containers** | **Docker & Docker Compose** | Local service reproducibility and multi-stage container builds |

---

## Repository Structure

```
CampusBite/
├── backend/
│   ├── cmd/
│   │   ├── migrate/          # Database migration CLI (up / down / status)
│   │   ├── seed/             # Test catalog & sample user seeder
│   │   └── server/           # Main Go HTTP server entrypoint
│   ├── internal/
│   │   ├── auth/             # JWT token service & bcrypt password hashing
│   │   ├── cache/            # Redis cache-aside client & Lua rate limiter
│   │   ├── config/           # Environment configuration loader
│   │   ├── database/         # PostgreSQL connection pool (pgxpool)
│   │   ├── handlers/         # HTTP endpoints & WebSocket handler
│   │   ├── middleware/       # JWT Auth, Role-Based Access Control (RBAC), Rate Limiting, CORS
│   │   ├── models/           # Domain models, request/response DTOs, intent allowlist
│   │   ├── repository/       # Parameterized SQL data access layer
│   │   ├── service/          # Razorpay, Gemini AI router, and analytics services
│   │   ├── worker/           # Background workers (reservation expiry & idempotency prune)
│   │   └── ws/               # WebSocket Hub, client pumps, connection registry
│   ├── migrations/           # Versioned SQL migration files (.up.sql / .down.sql)
│   ├── Dockerfile            # Multi-stage production Go backend container build
│   └── .dockerignore
├── frontend/
│   ├── src/
│   │   ├── components/       # Navbar, PaymentModal, shared UI components
│   │   ├── context/          # Auth, Cart, Notification, WebSocket React Contexts
│   │   ├── pages/            # Login, Menu, Cart, Orders, Detail, AdminDashboard
│   │   └── services/         # Type-safe API clients & WebSocket client
│   ├── vercel.json           # Vercel SPA routing configuration
│   ├── Dockerfile            # Multi-stage Nginx container build
│   ├── package.json
│   └── vite.config.js
├── docker-compose.yml        # Local PostgreSQL, Redis, and full-stack container profiles
├── .env.example              # Comprehensive environment template
└── README.md
```

---

## Running Locally

### 1. Start Database & Cache with Docker Compose

```bash
docker compose up -d
```
*Starts PostgreSQL on `localhost:5432` and Redis on `localhost:6379` with persistent volumes and health checks.*

### 2. Configure Environment

Copy `.env.example` to `.env` in the root directory:
```bash
cp .env.example .env
```

### 3. Run Database Migrations & Seed Demo Data

```bash
cd backend
go run ./cmd/migrate up
go run ./cmd/seed
```

### 4. Start the Go Backend Server

```bash
cd backend
go run ./cmd/server
```
*The backend starts listening on `http://localhost:8080` (Health check: `http://localhost:8080/health`).*

### 5. Start the React Frontend

```bash
cd frontend
npm install
npm run dev
```
*Open `http://localhost:5173` in your browser.*

---

## Running Full Stack in Docker (Optional)

To build and run the entire stack (PostgreSQL, Redis, Backend, Frontend) within isolated Docker containers:

```bash
docker compose --profile full up -d --build
```
- **Frontend:** `http://localhost:3000`
- **Backend API:** `http://localhost:8080`
- **PostgreSQL:** `localhost:5432`
- **Redis:** `localhost:6379`

To stop all containers:
```bash
docker compose --profile full down
```

---

## Production Deployment Guide

### A. Deploy Frontend on Vercel

1. **Push Code to GitHub / Git Provider**.
2. **Import Project into Vercel**:
   - Set **Root Directory** to `frontend`.
   - **Framework Preset**: `Vite`.
   - **Build Command**: `npm run build`.
   - **Output Directory**: `dist`.
3. **Set Environment Variables in Vercel Dashboard**:
   - `VITE_API_BASE_URL`: `https://<your-render-backend-url>.onrender.com/api/v1`
   - `VITE_WS_URL`: `wss://<your-render-backend-url>.onrender.com/api/v1/ws`
4. Deploy! The included `frontend/vercel.json` ensures client-side routing works seamlessly across all paths.

---

### B. Deploy Backend on Render (Web Service / Docker)

1. **Provision Managed PostgreSQL & Redis**:
   - Create a **PostgreSQL** instance (Render Postgres, Neon, or Supabase).
   - Create a **Redis** instance (Render Redis or Upstash).
2. **Create New Web Service on Render**:
   - Connect your GitHub repository.
   - **Root Directory**: `backend`.
   - **Runtime**: `Docker` (Render automatically detects `backend/Dockerfile`).
3. **Configure Environment Variables on Render**:
   ```env
   PORT=8080
   DATABASE_URL=postgres://<user>:<password>@<host>:<port>/<database>?sslmode=require
   REDIS_URL=rediss://<user>:<password>@<host>:<port>/0
   JWT_SECRET=<generate-a-cryptographically-secure-32-byte-string>
   RAZORPAY_KEY_ID=<your-razorpay-key-id>
   RAZORPAY_KEY_SECRET=<your-razorpay-key-secret>
   RAZORPAY_WEBHOOK_SECRET=<your-razorpay-webhook-secret>
   GEMINI_API_KEY=<your-google-gemini-api-key>
   GEMINI_MODEL=gemini-2.5-flash
   ```
4. **Health Check Path**: Set to `/health`.
5. **Run Migrations on Production Database**:
   - Run the migration CLI using Render One-Off Job / Shell:
     ```bash
     /app/migrate up
     ```

---

## Key System Guarantees

1. **Transactional Inventory Locking (`SELECT ... FOR UPDATE`):**
   - Stock deduction and temporary reservation are executed inside an ACID transaction.
   - Simultaneous checkout requests for the same limited item are serialized, eliminating race conditions and overselling.
2. **Idempotent Checkout (`Idempotency-Key`):**
   - Repeated requests with the same key for a user return the cached result without creating duplicate orders or reserving double inventory.
3. **WebSocket Reconnect & Offline Resilience:**
   - Real-time updates are delivered immediately over WebSockets.
   - If a client is offline, all events are permanently stored in the PostgreSQL `notifications` table, ensuring zero data loss upon reconnect.
4. **Natural-Language Analytics Security Boundary:**
   - Gemini translates admin business queries into structured intent codes.
   - The Go backend verifies the intent against a strict allowlist and executes predefined parameterized SQL queries. **Zero arbitrary text-to-SQL or direct database exposure.**

---

## Testing & Verification

```bash
# Backend Test Suite & Static Analysis
cd backend
go test -v ./...
go vet ./...
go build ./...

# Frontend Production Build
cd ../frontend
npm run build
```

---

## License

Private repository. All rights reserved.
