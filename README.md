# CampusBite

CampusBite is a high-performance food ordering and queue management system designed for campus cafeterias.

## Tech Stack

- **Language:** Go 1.26+
- **Framework:** Gin Web Framework
- **Database:** PostgreSQL 18 with `pgx/pgxpool`
- **Cache/Queue:** Redis 8
- **Containerization:** Docker Compose

## Project Structure

```
CampusBite/
├── backend/
│   ├── cmd/
│   │   ├── migrate/      # Database migrations runner
│   │   ├── seed/         # Database seeder
│   │   └── server/       # Main HTTP server entrypoint
│   ├── internal/
│   │   ├── auth/         # JWT generation & password hashing (bcrypt)
│   │   ├── config/       # Environment configuration loader
│   │   ├── database/     # PostgreSQL connection pool (pgxpool)
│   │   ├── handlers/     # HTTP handler functions
│   │   ├── middleware/   # JWT authentication & RBAC middleware
│   │   ├── models/       # Domain data models & request/response types
│   │   ├── repository/   # Data access layer
│   │   └── routes/       # API route registration
│   └── migrations/       # SQL schema migrations
├── docker-compose.yml    # PostgreSQL and Redis services
└── .gitignore
```

## Getting Started

### Prerequisites

- Go 1.26+
- Docker & Docker Compose

### 1. Infrastructure Setup

Start PostgreSQL and Redis services:

```bash
docker compose up -d
```

### 2. Environment Configuration

Create a `.env` file in the root directory:

```env
PORT=8080
DATABASE_URL=postgres://campusbite:campusbite_dev@localhost:5432/campusbite
REDIS_URL=redis://localhost:6379/0
JWT_SECRET=your-secure-jwt-secret
```

### 3. Run Migrations

```bash
cd backend
go run ./cmd/migrate
```

### 4. Start Backend Server

```bash
cd backend
go run ./cmd/server
```

## API Endpoints (Current Status)

- **Health Check:** `GET /health`
- **Auth:**
  - `POST /api/v1/auth/register` (Register student)
  - `POST /api/v1/auth/login` (Login & receive JWT)
  - `GET /api/v1/auth/me` (Protected)
- **Menu:**
  - `GET /api/v1/menu` (Public)
  - `GET /api/v1/menu/:id` (Public)
  - `POST /api/v1/menu` (Admin only)
  - `PATCH /api/v1/menu/:id` (Admin only)
  - `DELETE /api/v1/menu/:id` (Admin only)
- **Inventory:**
  - `GET /api/v1/inventory` (Public)
  - `GET /api/v1/inventory/:menuItemId` (Public)
  - `PATCH /api/v1/inventory/:menuItemId` (Admin only)
- **Cart:**
  - `GET /api/v1/cart` (Authenticated student)
  - `POST /api/v1/cart/items` (Add/increment item)
  - `PATCH /api/v1/cart/items/:menuItemId` (Update quantity)
  - `DELETE /api/v1/cart/items/:menuItemId` (Remove item)
  - `DELETE /api/v1/cart` (Clear cart)
- **Orders:**
  - `GET /api/v1/orders` (Student sees own, Admin sees all)
  - `GET /api/v1/orders/:id` (Order details with RBAC)
  - `POST /api/v1/orders` (Place order from cart)

## License

Private repository.
