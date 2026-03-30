# API Docs

This folder contains the API contract for the mobile client.

## Files

- `openapi.yaml` - main OpenAPI 3.0 contract for Gateway REST and WebSocket handshake.

## How to Use

1. Open in Swagger Editor:
   - https://editor.swagger.io/
   - paste `docs/api/openapi.yaml`

2. Generate client SDK for mobile:
   - Flutter/Dart: `dart-dio-next`
   - Kotlin: `kotlin`
   - Swift: `swift5`

3. Use environment-specific base URL:
   - local: `http://localhost:8080`
   - staging/prod: replace `servers[0].url`

## Auth Flow

1. `POST /v1/auth/login` -> receive `access_token` and `refresh_token`
2. Send `Authorization: Bearer <access_token>` for protected endpoints
3. On `401`, call `POST /v1/auth/refresh`
4. For realtime:
   - call `POST /v1/auth/ws-ticket`
   - connect `ws://<host>/ws?ticket=...`
