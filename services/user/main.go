package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	userv1 "messenger/services/user/internal/pb/proto/user/v1"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type userServer struct {
	userv1.UnimplementedUserServiceServer
	db *sqlx.DB
}

type userRow struct {
	ID        string    `db:"id"`
	Username  string    `db:"username"`
	Phone     string    `db:"phone"`
	CreatedAt time.Time `db:"created_at"`
}

func (s *userServer) GetUser(_ context.Context, req *userv1.GetUserRequest) (*userv1.UserResponse, error) {
	if req.GetUserId() == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	var row userRow
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

	var rows []userRow
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func startHTTPServer(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	log.Printf("user HTTP healthcheck started on :%s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func main() {
	port := getenv("USER_GRPC_PORT", "9002")
	httpPort := getenv("PORT", "8080")
	dsn := getenv("DATABASE_URL", "postgres://messenger:messenger@postgres:5432/messenger?sslmode=disable")

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}

	go startHTTPServer(httpPort)

	srv := grpc.NewServer()
	userv1.RegisterUserServiceServer(srv, &userServer{db: db})

	log.Printf("user gRPC started on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
