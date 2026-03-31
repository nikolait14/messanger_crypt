package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"time"

	"messenger/services/gateway/handler"
	authv1 "messenger/services/gateway/internal/pb/proto/auth/v1"
	messagev1 "messenger/services/gateway/internal/pb/proto/message/v1"
	userv1 "messenger/services/gateway/internal/pb/proto/user/v1"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type config struct {
	HTTPPort        string
	DatabaseURL     string
	JWTAccess       string
	JWTRefresh      string
	AccessTTL       time.Duration
	RefreshTTL      time.Duration
	BcryptCost      int
	EncryptionKey   string
	AuthGRPCPort    string
	UserGRPCPort    string
	MessageGRPCPort string
}

type tokenClaims struct {
	UserID   string `json:"uid"`
	FamilyID string `json:"fid,omitempty"`
	Type     string `json:"typ"`
	jwt.RegisteredClaims
}

type authServer struct {
	authv1.UnimplementedAuthServiceServer
	db  *sqlx.DB
	cfg config
}

type userServer struct {
	userv1.UnimplementedUserServiceServer
	db *sqlx.DB
}

type messageServer struct {
	messagev1.UnimplementedMessageServiceServer
	db      *sqlx.DB
	aead    cipher.AEAD
	keyID   int16
	keyRing map[int16]cipher.AEAD
}

type userRow struct {
	ID           uuid.UUID `db:"id"`
	Username     string    `db:"username"`
	Phone        string    `db:"phone"`
	PasswordHash string    `db:"password_hash"`
}

type userInfoRow struct {
	ID        string    `db:"id"`
	Username  string    `db:"username"`
	Phone     string    `db:"phone"`
	CreatedAt time.Time `db:"created_at"`
}

type historyRow struct {
	ID          string    `db:"id"`
	SenderID    string    `db:"sender_id"`
	ReceiverID  string    `db:"receiver_id"`
	Content     string    `db:"content"`
	KeyID       int16     `db:"key_id"`
	IsDelivered bool      `db:"is_delivered"`
	CreatedAt   time.Time `db:"created_at"`
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadConfig() (config, error) {
	accessTTL, err := time.ParseDuration(getenv("ACCESS_TOKEN_TTL", "60m"))
	if err != nil {
		return config{}, fmt.Errorf("invalid ACCESS_TOKEN_TTL: %w", err)
	}
	refreshTTL, err := time.ParseDuration(getenv("REFRESH_TOKEN_TTL", "720h"))
	if err != nil {
		return config{}, fmt.Errorf("invalid REFRESH_TOKEN_TTL: %w", err)
	}

	httpPort := getenv("GATEWAY_PORT", "")
	if httpPort == "" {
		httpPort = getenv("PORT", "8080")
	}

	cfg := config{
		HTTPPort:        httpPort,
		DatabaseURL:     getenv("DATABASE_URL", "postgres://messenger:messenger@postgres:5432/messenger?sslmode=disable"),
		JWTAccess:       getenv("JWT_ACCESS_SECRET", "change_me_access_secret"),
		JWTRefresh:      getenv("JWT_REFRESH_SECRET", "change_me_refresh_secret"),
		AccessTTL:       accessTTL,
		RefreshTTL:      refreshTTL,
		BcryptCost:      12,
		EncryptionKey:   getenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef"),
		AuthGRPCPort:    getenv("AUTH_GRPC_PORT", "9001"),
		UserGRPCPort:    getenv("USER_GRPC_PORT", "9002"),
		MessageGRPCPort: getenv("MESSAGE_GRPC_PORT", "9003"),
	}

	if len(cfg.EncryptionKey) != 32 {
		return config{}, errors.New("ENCRYPTION_KEY must be exactly 32 bytes")
	}

	return cfg, nil
}

func ensureSchema(ctx context.Context, db *sqlx.DB) error {
	const schema = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username TEXT NOT NULL UNIQUE,
    phone TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    public_key TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TABLE IF NOT EXISTS refresh_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    family_id UUID NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_exp ON refresh_tokens(user_id, expires_at DESC);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_family ON refresh_tokens(family_id);
CREATE TABLE IF NOT EXISTS messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    receiver_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    content TEXT NOT NULL,
    key_id SMALLINT NOT NULL DEFAULT 1,
    is_delivered BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_messages_receiver_delivery_time
  ON messages(receiver_id, is_delivered, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_messages_pair_time
  ON messages(sender_id, receiver_id, created_at DESC);
`
	_, err := db.ExecContext(ctx, schema)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *authServer) issueTokenPair(userID, familyID string) (string, string, int64, error) {
	now := time.Now()
	accessClaims := tokenClaims{
		UserID: userID,
		Type:   "access",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.AccessTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}
	refreshClaims := tokenClaims{
		UserID:   userID,
		FamilyID: familyID,
		Type:     "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ID:        uuid.NewString(),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.cfg.RefreshTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
	}

	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString([]byte(s.cfg.JWTAccess))
	if err != nil {
		return "", "", 0, err
	}
	refresh, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString([]byte(s.cfg.JWTRefresh))
	if err != nil {
		return "", "", 0, err
	}

	return access, refresh, int64(s.cfg.AccessTTL.Seconds()), nil
}

func (s *authServer) parseRefreshToken(token string) (*tokenClaims, error) {
	claims := &tokenClaims{}
	parsed, err := jwt.ParseWithClaims(token, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method != jwt.SigningMethodHS256 {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return []byte(s.cfg.JWTRefresh), nil
	})
	if err != nil || !parsed.Valid || claims.Type != "refresh" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *authServer) storeRefreshToken(userID, familyID, rawToken string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	fid, err := uuid.Parse(familyID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, NOW() + $4::interval)`,
		uid,
		fid,
		hashToken(rawToken),
		fmt.Sprintf("%d seconds", int(s.cfg.RefreshTTL.Seconds())),
	)
	return err
}

func (s *authServer) revokeFamily(familyID string) error {
	fid, err := uuid.Parse(familyID)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE refresh_tokens SET revoked_at = NOW() WHERE family_id = $1 AND revoked_at IS NULL`, fid)
	return err
}

func (s *authServer) Register(_ context.Context, req *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	if req.GetUsername() == "" || req.GetPhone() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "username, phone and password are required")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.GetPassword()), s.cfg.BcryptCost)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to hash password")
	}

	const q = `
INSERT INTO users (username, phone, password_hash)
VALUES ($1, $2, $3)
RETURNING id, username, phone;
`
	var created struct {
		ID       uuid.UUID `db:"id"`
		Username string    `db:"username"`
		Phone    string    `db:"phone"`
	}
	err = s.db.Get(&created, q, req.GetUsername(), req.GetPhone(), string(hash))
	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, status.Error(codes.AlreadyExists, "username or phone already exists")
		}
		return nil, status.Error(codes.Internal, "failed to create user")
	}

	return &authv1.RegisterResponse{
		UserId:   created.ID.String(),
		Username: created.Username,
		Phone:    created.Phone,
	}, nil
}

func (s *authServer) Login(_ context.Context, req *authv1.LoginRequest) (*authv1.LoginResponse, error) {
	if req.GetUsername() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "username and password are required")
	}

	var user userRow
	err := s.db.Get(&user, "SELECT id, username, phone, password_hash FROM users WHERE username = $1", req.GetUsername())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.Unauthenticated, "invalid credentials")
		}
		return nil, status.Error(codes.Internal, "failed to load user")
	}

	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.GetPassword())) != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	familyID := uuid.NewString()
	access, refresh, expiresIn, err := s.issueTokenPair(user.ID.String(), familyID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to issue tokens")
	}
	if err := s.storeRefreshToken(user.ID.String(), familyID, refresh); err != nil {
		return nil, status.Error(codes.Internal, "failed to persist refresh token")
	}

	return &authv1.LoginResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    expiresIn,
	}, nil
}

func (s *authServer) Refresh(_ context.Context, req *authv1.RefreshRequest) (*authv1.RefreshResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	claims, err := s.parseRefreshToken(req.GetRefreshToken())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	var current struct {
		UserID   uuid.UUID `db:"user_id"`
		FamilyID uuid.UUID `db:"family_id"`
	}
	err = s.db.Get(
		&current,
		`SELECT user_id, family_id FROM refresh_tokens
		 WHERE token_hash = $1 AND revoked_at IS NULL AND expires_at > NOW()`,
		hashToken(req.GetRefreshToken()),
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.Unauthenticated, "refresh token revoked or expired")
		}
		return nil, status.Error(codes.Internal, "failed to validate refresh token")
	}

	if claims.UserID != current.UserID.String() || claims.FamilyID != current.FamilyID.String() {
		return nil, status.Error(codes.Unauthenticated, "refresh token does not match session")
	}

	if err := s.revokeFamily(claims.FamilyID); err != nil {
		return nil, status.Error(codes.Internal, "failed to rotate refresh token")
	}

	access, refresh, expiresIn, err := s.issueTokenPair(claims.UserID, claims.FamilyID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to issue rotated tokens")
	}
	if err := s.storeRefreshToken(claims.UserID, claims.FamilyID, refresh); err != nil {
		return nil, status.Error(codes.Internal, "failed to store rotated token")
	}

	return &authv1.RefreshResponse{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    expiresIn,
	}, nil
}

func (s *authServer) Logout(_ context.Context, req *authv1.LogoutRequest) (*authv1.LogoutResponse, error) {
	if req.GetRefreshToken() == "" {
		return nil, status.Error(codes.InvalidArgument, "refresh_token is required")
	}

	claims, err := s.parseRefreshToken(req.GetRefreshToken())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if err := s.revokeFamily(claims.FamilyID); err != nil {
		return nil, status.Error(codes.Internal, "failed to revoke session")
	}

	return &authv1.LogoutResponse{Success: true}, nil
}

func (s *userServer) GetUser(_ context.Context, req *userv1.GetUserRequest) (*userv1.UserResponse, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	var row userInfoRow
	err := s.db.Get(&row, `SELECT id::text, username, phone, created_at FROM users WHERE id = $1`, req.GetUserId())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "user not found")
		}
		return nil, status.Error(codes.Internal, "failed to load user")
	}

	return &userv1.UserResponse{
		Id:        row.ID,
		Username:  row.Username,
		Phone:     row.Phone,
		CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *userServer) SearchUsers(_ context.Context, req *userv1.SearchUsersRequest) (*userv1.SearchUsersResponse, error) {
	if req.GetUsername() == "" {
		return nil, status.Error(codes.InvalidArgument, "username is required")
	}

	limit := req.GetLimit()
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	var rows []userInfoRow
	err := s.db.Select(
		&rows,
		`SELECT id::text, username, phone, created_at
		 FROM users
		 WHERE username ILIKE $1
		 ORDER BY created_at DESC
		 LIMIT $2`,
		req.GetUsername()+"%",
		limit,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to search users")
	}

	resp := &userv1.SearchUsersResponse{Users: make([]*userv1.UserResponse, 0, len(rows))}
	for _, row := range rows {
		resp.Users = append(resp.Users, &userv1.UserResponse{
			Id:        row.ID,
			Username:  row.Username,
			Phone:     row.Phone,
			CreatedAt: row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	return resp, nil
}

func encryptText(aead cipher.AEAD, text string) (string, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, []byte(text), nil)
	payload := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func (s *messageServer) decryptByKeyID(keyID int16, encoded string) (string, error) {
	aead, ok := s.keyRing[keyID]
	if !ok {
		return "", errors.New("unknown key id")
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	if len(raw) < 12 {
		return "", errors.New("invalid ciphertext")
	}
	nonce := raw[:12]
	ciphertext := raw[12:]
	plain, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func (s *messageServer) SendMessage(_ context.Context, req *messagev1.SendMessageRequest) (*messagev1.SendMessageResponse, error) {
	if req.GetSenderId() == "" || req.GetToUsername() == "" || req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "sender_id, to_username and text are required")
	}

	var receiverID string
	err := s.db.Get(&receiverID, `SELECT id::text FROM users WHERE username = $1`, req.GetToUsername())
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Error(codes.NotFound, "receiver not found")
		}
		return nil, status.Error(codes.Internal, "failed to resolve receiver")
	}

	ciphertext, err := encryptText(s.aead, req.GetText())
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to encrypt message")
	}

	var out struct {
		ID        string    `db:"id"`
		CreatedAt time.Time `db:"created_at"`
	}
	err = s.db.Get(
		&out,
		`INSERT INTO messages (sender_id, receiver_id, content, key_id, is_delivered)
		 VALUES ($1, $2, $3, $4, false)
		 RETURNING id::text, created_at`,
		req.GetSenderId(),
		receiverID,
		ciphertext,
		s.keyID,
	)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to store message")
	}

	return &messagev1.SendMessageResponse{
		MessageId: out.ID,
		CreatedAt: out.CreatedAt.UTC().Format(time.RFC3339),
	}, nil
}

func (s *messageServer) GetHistory(_ context.Context, req *messagev1.GetHistoryRequest) (*messagev1.GetHistoryResponse, error) {
	if req.GetUserId() == "" || req.GetPeerUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id and peer_user_id are required")
	}

	limit := req.GetLimit()
	if limit <= 0 || limit > 100 {
		limit = 50
	}

	query := `
SELECT id::text, sender_id::text, receiver_id::text, content, key_id, is_delivered, created_at
FROM messages
WHERE ((sender_id = $1 AND receiver_id = $2) OR (sender_id = $2 AND receiver_id = $1))
`
	args := []interface{}{req.GetUserId(), req.GetPeerUserId()}
	if req.GetCursor() != "" {
		query += ` AND created_at < COALESCE((SELECT created_at FROM messages WHERE id = $3::uuid), NOW() + interval '1 second')`
		args = append(args, req.GetCursor())
	}
	query += ` ORDER BY created_at DESC LIMIT $4`
	if req.GetCursor() != "" {
		args = append(args, limit)
	} else {
		args = append(args, limit)
	}
	if req.GetCursor() == "" {
		query = `
SELECT id::text, sender_id::text, receiver_id::text, content, key_id, is_delivered, created_at
FROM messages
WHERE ((sender_id = $1 AND receiver_id = $2) OR (sender_id = $2 AND receiver_id = $1))
ORDER BY created_at DESC LIMIT $3`
		args = []interface{}{req.GetUserId(), req.GetPeerUserId(), limit}
	}

	var rows []historyRow
	if err := s.db.Select(&rows, query, args...); err != nil {
		return nil, status.Error(codes.Internal, "failed to load history")
	}

	resp := &messagev1.GetHistoryResponse{Messages: make([]*messagev1.MessageItem, 0, len(rows))}
	for _, row := range rows {
		text, err := s.decryptByKeyID(row.KeyID, row.Content)
		if err != nil {
			return nil, status.Error(codes.Internal, "failed to decrypt message")
		}
		resp.Messages = append(resp.Messages, &messagev1.MessageItem{
			Id:          row.ID,
			SenderId:    row.SenderID,
			ReceiverId:  row.ReceiverID,
			Text:        text,
			IsDelivered: row.IsDelivered,
			CreatedAt:   row.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	if len(rows) > 0 {
		resp.NextCursor = rows[len(rows)-1].ID
	}

	_, _ = s.db.Exec(
		`UPDATE messages SET is_delivered = true
		 WHERE receiver_id = $1::uuid AND sender_id = $2::uuid AND is_delivered = false`,
		req.GetUserId(),
		req.GetPeerUserId(),
	)
	return resp, nil
}

func startGRPCServer(addr string, register func(*grpc.Server), name string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := grpc.NewServer()
	register(srv)
	go func() {
		log.Printf("%s gRPC started on %s", name, addr)
		if err := srv.Serve(lis); err != nil {
			log.Fatalf("%s gRPC failed: %v", name, err)
		}
	}()
	return nil
}

func dialGRPC(ctx context.Context, target string) (*grpc.ClientConn, error) {
	return grpc.DialContext(
		ctx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}

	db, err := sqlx.Connect("postgres", cfg.DatabaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ensureSchema(ctx, db); err != nil {
		log.Fatal(err)
	}

	block, err := aes.NewCipher([]byte(cfg.EncryptionKey))
	if err != nil {
		log.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		log.Fatal(err)
	}

	authAddr := "127.0.0.1:" + cfg.AuthGRPCPort
	userAddr := "127.0.0.1:" + cfg.UserGRPCPort
	messageAddr := "127.0.0.1:" + cfg.MessageGRPCPort

	if err := startGRPCServer(authAddr, func(s *grpc.Server) {
		authv1.RegisterAuthServiceServer(s, &authServer{db: db, cfg: cfg})
	}, "auth"); err != nil {
		log.Fatal(err)
	}
	if err := startGRPCServer(userAddr, func(s *grpc.Server) {
		userv1.RegisterUserServiceServer(s, &userServer{db: db})
	}, "user"); err != nil {
		log.Fatal(err)
	}
	if err := startGRPCServer(messageAddr, func(s *grpc.Server) {
		messagev1.RegisterMessageServiceServer(s, &messageServer{
			db:      db,
			aead:    aead,
			keyID:   1,
			keyRing: map[int16]cipher.AEAD{1: aead},
		})
	}, "message"); err != nil {
		log.Fatal(err)
	}

	dialCtx, cancelDial := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelDial()

	authConn, err := dialGRPC(dialCtx, authAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer authConn.Close()
	userConn, err := dialGRPC(dialCtx, userAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer userConn.Close()
	messageConn, err := dialGRPC(dialCtx, messageAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer messageConn.Close()

	handler.SetJWTAccessSecret(cfg.JWTAccess)
	handler.SetAuthClient(authv1.NewAuthServiceClient(authConn))
	handler.SetUserClient(userv1.NewUserServiceClient(userConn))
	handler.SetMessageClient(messagev1.NewMessageServiceClient(messageConn))

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	handler.RegisterRoutes(r)

	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}

	addr := ":" + cfg.HTTPPort
	log.Printf("monolith gateway started on %s", addr)
	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
