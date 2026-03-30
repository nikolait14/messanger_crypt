package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"log"
	"net"
	"os"
	"time"

	messagev1 "messenger/services/message/internal/pb/proto/message/v1"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type messageServer struct {
	messagev1.UnimplementedMessageServiceServer
	db      *sqlx.DB
	aead    cipher.AEAD
	keyID   int16
	keyRing map[int16]cipher.AEAD
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
		// Move limit arg to correct placeholder index for non-cursor query.
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

func encryptText(aead cipher.AEAD, text string) (string, error) {
	nonce := make([]byte, 12)
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := aead.Seal(nil, nonce, []byte(text), nil)
	payload := append(nonce, ct...)
	return base64.StdEncoding.EncodeToString(payload), nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func ensureMessageSchema(ctx context.Context, db *sqlx.DB) error {
	const schema = `
CREATE EXTENSION IF NOT EXISTS pgcrypto;
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

func main() {
	port := getenv("MESSAGE_GRPC_PORT", "9003")
	dsn := getenv("DATABASE_URL", "postgres://messenger:messenger@postgres:5432/messenger?sslmode=disable")
	key := getenv("ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	keyID := int16(1)

	if len(key) != 32 {
		log.Fatal("ENCRYPTION_KEY must be exactly 32 bytes")
	}

	block, err := aes.NewCipher([]byte(key))
	if err != nil {
		log.Fatal(err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		log.Fatal(err)
	}

	db, err := sqlx.Connect("postgres", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := ensureMessageSchema(ctx, db); err != nil {
		log.Fatal(err)
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatal(err)
	}

	srv := grpc.NewServer()
	messagev1.RegisterMessageServiceServer(srv, &messageServer{
		db:      db,
		aead:    aead,
		keyID:   keyID,
		keyRing: map[int16]cipher.AEAD{keyID: aead},
	})

	log.Printf("message gRPC started on :%s", port)
	if err := srv.Serve(lis); err != nil {
		log.Fatal(err)
	}
}
