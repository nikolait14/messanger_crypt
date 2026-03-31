# Railway Monorepo Setup

Use one GitHub repository and one Railway service per backend component.

## Monolith Mode (Recommended For Simpler Deploy)

You can deploy only one service (`gateway`) as a monolith.

- Config as Code path: `/services/gateway/railway.monolith.toml`
- Dockerfile: `services/gateway/Dockerfile.monolith`

Required variables for monolith:

- `PORT=8080`
- `DATABASE_URL=<reference from Railway Postgres>`
- `JWT_ACCESS_SECRET=<secret>`
- `JWT_REFRESH_SECRET=<secret>`
- `ENCRYPTION_KEY=<32 chars>`
- `ACCESS_TOKEN_TTL=60m`
- `REFRESH_TOKEN_TTL=720h`
- `AUTH_GRPC_PORT=9001` (internal in-process)
- `USER_GRPC_PORT=9002` (internal in-process)
- `MESSAGE_GRPC_PORT=9003` (internal in-process)

In this mode, no separate `auth/user/message` Railway services are needed.

## Config Paths

When creating each Railway service, set **Config as Code path**:

- `gateway`: `/services/gateway/railway.toml`
- `auth`: `/services/auth/railway.toml`
- `user`: `/services/user/railway.toml`
- `message`: `/services/message/railway.toml`

Each config uses Docker build with the correct Dockerfile path.

## Required Variables

- `gateway`:
  - `PORT=8080`
  - `GATEWAY_PORT=8080` (optional override)
  - `AUTH_GRPC_ADDR=<auth-private-host>:9001`
  - `USER_GRPC_ADDR=<user-private-host>:9002`
  - `MESSAGE_GRPC_ADDR=<message-private-host>:9003`
  - `JWT_ACCESS_SECRET=<same as auth>`

- `auth`:
  - `PORT=8080` (HTTP healthcheck)
  - `AUTH_GRPC_PORT=9001`
  - `DATABASE_URL=<reference from Railway Postgres>`
  - `JWT_ACCESS_SECRET=<secret>`
  - `JWT_REFRESH_SECRET=<secret>`
  - `ACCESS_TOKEN_TTL=60m`
  - `REFRESH_TOKEN_TTL=720h`

- `user`:
  - `PORT=8080` (HTTP healthcheck)
  - `USER_GRPC_PORT=9002`
  - `DATABASE_URL=<reference from Railway Postgres>`

- `message`:
  - `PORT=8080` (HTTP healthcheck)
  - `MESSAGE_GRPC_PORT=9003`
  - `DATABASE_URL=<reference from Railway Postgres>`
  - `ENCRYPTION_KEY=<32 chars>`

Only `gateway` should have a public domain for API access.
