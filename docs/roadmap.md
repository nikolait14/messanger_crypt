# Техническое задание — Мессенджер

**Версия 1.1 · 2026**

---

## 1. Общее описание

Мессенджер — исследовательский backend-проект на Go, реализующий защищённую систему обмена текстовыми сообщениями в реальном времени. Архитектура построена по принципу микросервисов с коммуникацией через gRPC и единой точкой входа через API Gateway.

---

## 2. Технический стек

**Язык:** Go 1.22+

**HTTP роутер:** Chi — REST эндпоинты на Gateway

**WebSocket:** gorilla/websocket — реалтайм доставка сообщений

**Inter-service:** gRPC + Protobuf — связь между микросервисами

**База данных:** PostgreSQL + sqlx, миграции через goose

**Аутентификация:** JWT (golang-jwt) — access + refresh токены

**Пароли:** bcrypt (cost factor 12)

**Шифрование БД:** AES-256-GCM — контент сообщений

**Логирование:** log/slog — структурированные логи

**Контейнеры:** Docker + Docker Compose

**Деплой:** Railway.app — HTTPS/WSS автоматически через Let's Encrypt

---

## 3. Архитектура

### Микросервисы

| Сервис | Порт | Ответственность |
|---|---|---|
| API Gateway | :8080 | Единая точка входа, маршрутизация, JWT-валидация |
| Auth Service | :9001 (gRPC) | Регистрация, логин, выдача и обновление токенов |
| User Service | :9002 (gRPC) | Профили, поиск по username |
| Message Service | :9003 (gRPC) | WebSocket Hub, онлайн-статус, отправка и хранение сообщений |

### Схема взаимодействия

Клиент общается только с API Gateway по REST и WebSocket. Gateway валидирует JWT и проксирует запрос к нужному сервису по gRPC. Сервисы не знают о клиенте и не общаются напрямую друг с другом.

### Протоколы

- REST HTTP — регистрация, логин, поиск, история сообщений
- WebSocket (WSS) — реалтайм доставка, события статуса онлайн/офлайн (через одноразовый ws-ticket)
- gRPC + Protobuf — внутренняя коммуникация между Gateway и сервисами

---

## 4. Функциональные требования

### 4.1 Аутентификация

- Регистрация по номеру телефона, уникальному username и паролю (без SMS-верификации в MVP)
- Логин по username + пароль
- JWT access token (TTL: 60 минут) + refresh token (TTL: 30 дней)
- Refresh token хранится в БД в виде SHA-256 хэша
- Refresh token обновляется по схеме rotation (одноразовое использование refresh token)
- Логаут — инвалидация текущей token family (или конкретной сессии) в БД
- Пароли хранятся исключительно в виде bcrypt хэша

### 4.2 Профили и поиск

- Поиск пользователя по username: `GET /users/search?username=...`
- Просмотр профиля: `GET /users/:id` — username, маскированный телефон, статус онлайн
- Статус онлайн/офлайн хранится в памяти Hub в Message Service и обновляется при подключении/отключении WebSocket

### 4.3 Сообщения

- Отправка текста через WebSocket: `{"to": "username", "text": "..."}`
- Подключение к WebSocket по одноразовому `ws-ticket` (TTL 30–60 секунд), полученному по access token
- Доставка в реальном времени если получатель онлайн
- Сохранение в PostgreSQL в зашифрованном виде (AES-256-GCM) даже если получатель офлайн
- Доставка накопленных сообщений при следующем подключении получателя
- История переписки с cursor-пагинацией: `GET /messages/:userId?limit=50&cursor=<message_id>`
- Только личные сообщения (1 на 1), групповые чаты не входят в MVP

---

## 5. Безопасность

### 5.1 Шифрование в транзите
Весь трафик защищён TLS 1.3. WebSocket работает по WSS. Сертификат предоставляется Railway автоматически.

### 5.2 Шифрование сообщений в БД
- Алгоритм: AES-256-GCM (аутентифицированное шифрование)
- Ключ: 32 байта, хранится в переменной окружения `ENCRYPTION_KEY`, никогда не попадает в код
- Nonce: 12 байт случайных данных, генерируется для каждого сообщения
- Для ротации ключа каждое сообщение хранит `key_id` (версию ключа)
- Формат в БД: `base64(nonce + ciphertext)`
- При утечке дампа БД — сообщения нечитаемы без ключа

### 5.3 Заготовка под E2E
Поле `public_key` в таблице `users` и тип `TEXT` в поле `content` таблицы `messages` позволят перейти на E2E (X25519 + Double Ratchet) без изменения схемы БД.

### 5.4 Прочее
- Rate limiting на авторизацию — не более 10 попыток в минуту с одного IP
- Все входящие данные валидируются на сервере
- SQL-инъекции исключены через параметризованные запросы (sqlx)

---

## 6. Схема базы данных

### users
| Поле | Тип | Описание |
|---|---|---|
| id | UUID, PK | gen_random_uuid() |
| username | TEXT, UNIQUE | Уникальный никнейм |
| phone | TEXT, UNIQUE | Номер телефона |
| password | TEXT | bcrypt хэш |
| public_key | TEXT, nullable | Публичный ключ (для E2E) |
| created_at | TIMESTAMPTZ | Дата регистрации |

### messages
| Поле | Тип | Описание |
|---|---|---|
| id | UUID, PK | gen_random_uuid() |
| sender_id | UUID, FK | Ссылка на users.id |
| receiver_id | UUID, FK | Ссылка на users.id |
| content | TEXT | AES-256-GCM зашифрованный текст |
| key_id | SMALLINT | Версия ключа шифрования |
| is_delivered | BOOLEAN | Доставлено получателю |
| created_at | TIMESTAMPTZ | Время отправки |

### refresh_tokens
| Поле | Тип | Описание |
|---|---|---|
| id | UUID, PK | Идентификатор |
| user_id | UUID, FK | Ссылка на users.id |
| family_id | UUID | Идентификатор token family |
| token_hash | TEXT, UNIQUE | SHA-256 хэш токена |
| expires_at | TIMESTAMPTZ | Срок действия (30 дней) |
| revoked_at | TIMESTAMPTZ, nullable | Время отзыва токена/сессии |
| created_at | TIMESTAMPTZ | Дата создания |

### Рекомендуемые индексы
- `CREATE INDEX idx_messages_receiver_delivery_time ON messages(receiver_id, is_delivered, created_at DESC);`
- `CREATE INDEX idx_messages_pair_time ON messages(sender_id, receiver_id, created_at DESC);`
- `CREATE INDEX idx_refresh_tokens_user_exp ON refresh_tokens(user_id, expires_at DESC);`

---

## 7. API эндпоинты

### Публичные (без JWT)
| Метод | Путь | Описание |
|---|---|---|
| POST | /auth/register | Регистрация |
| POST | /auth/login | Логин, получение токенов |
| POST | /auth/refresh | Обновление access token + rotation refresh token |
| POST | /auth/logout | Инвалидация refresh token (или token family) |

### Защищённые (требуют JWT)
| Метод | Путь | Описание |
|---|---|---|
| GET | /users/search?username=... | Поиск по username |
| GET | /users/:id | Профиль пользователя |
| GET | /users/me | Профиль текущего пользователя |
| GET | /messages/:userId | История переписки |
| POST | /auth/ws-ticket | Выдача одноразового ws-ticket (TTL 30–60 сек) |
| WS | /ws?ticket=... | WebSocket соединение по ticket |

---

## 8. Структура проекта

```
messenger/
├── services/
│   ├── gateway/        # API Gateway
│   ├── auth/           # Auth Service
│   ├── user/           # User Service
│   └── message/        # Message Service + WS Hub
├── proto/              # Protobuf определения
├── shared/             # Общие утилиты (crypto, jwt)
├── docker-compose.yml
├── Makefile            # make run, make migrate, make proto
└── .env.example
```

Каждый сервис внутри: `config/`, `db/`, `domain/`, `repository/`, `service/`, `handler/`

---

## 9. Роадмап

| # | Фаза | Задачи | Срок |
|---|---|---|---|
| 1 | Каркас проекта + Gateway + Docker | Базовая структура, docker-compose, healthcheck, маршрутизация Gateway | ~1 неделя |
| 2 | Авторизация | БД, миграции, register/login, JWT, refresh rotation | ~1 неделя |
| 3 | Профили и поиск | User Service, поиск по username, профиль | ~4–5 дней |
| 4 | WebSocket и чат | Hub, ws-ticket, отправка, offline delivery, cursor-история, AES + key_id | ~1.5 недели |
| 5 | Полировка | Graceful shutdown, логи, README, подготовка к деплою | ~3–4 дня |

**Общий срок MVP: 5–6 недель**

---

## 10. Mobile-ready backend

### 10.1 Сессии устройств
- Добавить учёт сессий на уровне устройства: `device_id`, `platform`, `app_version`, `last_seen_at`
- Привязывать refresh token family к конкретному устройству
- Поддержать логаут одного устройства и логаут всех устройств

### 10.2 Надёжная синхронизация сообщений
- Добавить `client_message_id` (UUID от клиента) для идемпотентной отправки при повторных запросах
- Добавить endpoint инкрементальной синхронизации: `GET /messages/sync?cursor=...&limit=...`
- Использовать монотонный server cursor (например, `created_at + id`) для безопасной догрузки истории

### 10.3 Статусы доставки
- В MVP минимум `sent` и `delivered`; `read` можно добавить после MVP
- Для входящих WS-сообщений предусмотреть ACK от клиента, чтобы корректно обновлять `is_delivered`

### 10.4 Push-ready архитектура
- Даже без push в MVP публиковать доменные события (`message.created`, `message.delivered`) через outbox-паттерн
- Это позволит позже подключить FCM/APNs без изменения core-логики Message Service

### 10.5 Mobile-ориентированная безопасность
- Для auth rate limiting учитывать не только IP, но и связку `user_id + device_id`
- Лимитировать выпуск `ws-ticket` на устройство (чтобы избежать спама переподключений)

### 10.6 Версионирование API
- REST сразу вести под префиксом `/v1` (например, `/v1/auth/login`, `/v1/messages/sync`)
- В `proto` использовать versioned package (`messenger.auth.v1`, `messenger.message.v1`)

---

## 11. Вне scope MVP

- SMS-верификация номера телефона
- Групповые чаты
- Отправка медиафайлов и фотографий
- Push-уведомления
- E2E-шифрование (исследовательская фаза после MVP)
- Мобильный или веб-клиент
- Kubernetes оркестрация
