# Railway Monorepo Setup

Use one GitHub repository and one Railway service per backend component.

## Config Paths

When creating each Railway service, set **Config as Code path**:

- `gateway`: `/services/gateway/railway.toml`
- `auth`: `/services/auth/railway.toml`
- `user`: `/services/user/railway.toml`
- `message`: `/services/message/railway.toml`

Each config uses Docker build with the correct Dockerfile path.

## Required Variables

- `gateway`:
  - `GATEWAY_PORT=8080`
  - `AUTH_GRPC_ADDR=<auth-private-host>:9001`
  - `USER_GRPC_ADDR=<user-private-host>:9002`
  - `MESSAGE_GRPC_ADDR=<message-private-host>:9003`
  - `JWT_ACCESS_SECRET=<same as auth>`

- `auth`:
  - `AUTH_GRPC_PORT=9001`
  - `DATABASE_URL=<reference from Railway Postgres>`
  - `JWT_ACCESS_SECRET=<secret>`
  - `JWT_REFRESH_SECRET=<secret>`
  - `ACCESS_TOKEN_TTL=60m`
  - `REFRESH_TOKEN_TTL=720h`

- `user`:
  - `USER_GRPC_PORT=9002`
  - `DATABASE_URL=<reference from Railway Postgres>`

- `message`:
  - `MESSAGE_GRPC_PORT=9003`
  - `DATABASE_URL=<reference from Railway Postgres>`
  - `ENCRYPTION_KEY=<32 chars>`

Only `gateway` should have a public domain for API access.
