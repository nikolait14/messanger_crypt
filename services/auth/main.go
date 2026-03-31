package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	authv1 "messenger/services/auth/internal/pb/proto/auth/v1"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
	"github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type authServer struct {
	authv1.UnimplementedAuthServiceServer
	mu  sync.RWMutex
	db  *sqlx.DB
	cfg config
}

// setDB stores the database handle once it becomes available.
func (s *authServer) setDB(db *sqlx.DB) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.db = db
}

// getDB returns the database handle, or an UNAVAILABLE gRPC error if the
// database connection has not been established yet.
func (s *authServer) getDB() (*sqlx.DB, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, status.Error(codes.Unavailable, "database not ready, please retry")
	}
	return s.db, nil
}

type config struct {
	GRPCPort      string
	DatabaseURL   string
	AccessSecret  string
	RefreshSecret string
	AccessTTL     time.Duration
	RefreshTTL    time.Duration
	BcryptCost    int
}

type userRow struct {
	ID           uuid.UUID `db:"id"`
	Username     string    `db:"username"`
	Phone        string    `db:"phone"`
	PasswordHash string    `db:"password_hash"`
}

type tokenClaims struct {
	UserID   string `json:"uid"`
	FamilyID string `json:"fid,omitempty"`
	Type     string `json:"typ"`
	jwt.RegisteredClaims
}

func (s *authServer) Register(_ context.Context, req *authv1.RegisterRequest) (*authv1.RegisterResponse, error) {
	if req.GetUsername() == "" || req.GetPhone() == "" || req.GetPassword() == "" {
		return nil, status.Error(codes.InvalidArgument, "username, phone and password are required")
	}

	db, err := s.getDB()
	if err != nil {
		return nil, err
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
	err = db.Get(&created, q, req.GetUsername(), req.GetPhone(), string(hash))
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

	db, err := s.getDB()
	if err != nil {
		return nil, err
	}

	var user userRow
	err = db.Get(&user, "SELECT id, username, phone, password_hash FROM users WHERE username = $1", req.GetUsername())
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
	if err := s.storeRefreshToken(db, user.ID.String(), familyID, refresh); err != nil {
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

	db, err := s.getDB()
	if err != nil {
		return nil, err
	}

	claims, err := s.parseRefreshToken(req.GetRefreshToken())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	var current struct {
		UserID   uuid.UUID `db:"user_id"`
		FamilyID uuid.UUID `db:"family_id"`
	}
	err = db.Get(
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

	if err := s.revokeFamily(db, claims.FamilyID); err != nil {
		return nil, status.Error(codes.Internal, "failed to rotate refresh token")
	}

	access, refresh, expiresIn, err := s.issueTokenPair(claims.UserID, claims.FamilyID)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to issue rotated tokens")
	}
	if err := s.storeRefreshToken(db, claims.UserID, claims.FamilyID, refresh); err != nil {
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

	db, err := s.getDB()
	if err != nil {
		return nil, err
	}

	claims, err := s.parseRefreshToken(req.GetRefreshToken())
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}
	if err := s.revokeFamily(db, claims.FamilyID); err != nil {
		return nil, status.Error(codes.Internal, "failed to revoke session")
	}

	return &authv1.LogoutResponse{Success: true}, nil
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
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

	cfg := config{
		GRPCPort:      getenv("AUTH_GRPC_PORT", "9001"),
		DatabaseURL:   getenv("DATABASE_URL", "postgres://messenger:messenger@postgres:5432/messenger?sslmode=disable"),
		AccessSecret:  getenv("JWT_ACCESS_SECRET", "change_me_access_secret"),
		RefreshSecret: getenv("JWT_REFRESH_SECRET", "change_me_refresh_secret"),
		AccessTTL:     accessTTL,
		RefreshTTL:    refreshTTL,
		BcryptCost:    12,
	}
	return cfg, nil
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

	access, err := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims).SignedString([]byte(s.cfg.AccessSecret))
	if err != nil {
		return "", "", 0, err
	}
	refresh, err := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims).SignedString([]byte(s.cfg.RefreshSecret))
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
		return []byte(s.cfg.RefreshSecret), nil
	})
	if err != nil || !parsed.Valid || claims.Type != "refresh" {
		return nil, errors.New("invalid token")
	}
	return claims, nil
}

func (s *authServer) storeRefreshToken(db *sqlx.DB, userID, familyID, rawToken string) error {
	uid, err := uuid.Parse(userID)
	if err != nil {
		return err
	}
	fid, err := uuid.Parse(familyID)
	if err != nil {
		return err
	}
	_, err = db.Exec(
		`INSERT INTO refresh_tokens (user_id, family_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, NOW() + $4::interval)`,
		uid,
		fid,
		hashToken(rawToken),
		fmt.Sprintf("%d seconds", int(s.cfg.RefreshTTL.Seconds())),
	)
	return err
}

func (s *authServer) revokeFamily(db *sqlx.DB, familyID string) error {
	fid, err := uuid.Parse(familyID)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE refresh_tokens SET revoked_at = NOW() WHERE family_id = $1 AND revoked_at IS NULL`, fid)
	return err
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
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
`
	_, err := db.ExecContext(ctx, schema)
	return err
}

func startHTTPServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("HTTP healthcheck started on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatal(err)
	}
	httpPort := getenv("PORT", "8080")

	// Start the HTTP healthcheck server immediately so Railway's healthcheck
	// can reach /v1/healthz before the database connection is established.
	go startHTTPServer(httpPort)

	addr := ":" + cfg.GRPCPort

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatal(err)
	}

	srv := &authServer{cfg: cfg}

	// Connect to the database and initialise the schema in the background.
	// The gRPC server starts right away; handlers will receive an UNAVAILABLE
	// error until the database is ready.
	go func() {
		const retryInterval = 5 * time.Second
		for {
			db, err := sqlx.Connect("postgres", cfg.DatabaseURL)
			if err != nil {
				log.Printf("db connect failed, retrying in %s: %v", retryInterval, err)
				time.Sleep(retryInterval)
				continue
			}

			schemaCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := ensureSchema(schemaCtx, db); err != nil {
				cancel()
				db.Close()
				log.Printf("schema init failed, retrying in %s: %v", retryInterval, err)
				time.Sleep(retryInterval)
				continue
			}
			cancel()

			srv.setDB(db)
			log.Printf("database ready")
			return
		}
	}()

	grpcServer := grpc.NewServer()
	authv1.RegisterAuthServiceServer(grpcServer, srv)

	log.Printf("auth gRPC started on %s", addr)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
