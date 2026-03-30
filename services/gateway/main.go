package main

import (
	"context"
	"log"
	"time"

	"messenger/services/gateway/config"
	"messenger/services/gateway/handler"
	authv1 "messenger/services/gateway/internal/pb/proto/auth/v1"
	messagev1 "messenger/services/gateway/internal/pb/proto/message/v1"
	userv1 "messenger/services/gateway/internal/pb/proto/user/v1"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	cfg := config.Load()
	handler.SetJWTAccessSecret(cfg.JWTSecret)

	authDialCtx, cancelAuth := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelAuth()
	authConn, err := grpc.DialContext(
		authDialCtx,
		cfg.AuthGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("failed to connect to auth service: %v", err)
	}
	defer authConn.Close()
	handler.SetAuthClient(authv1.NewAuthServiceClient(authConn))

	userDialCtx, cancelUser := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelUser()
	userConn, err := grpc.DialContext(
		userDialCtx,
		cfg.UserGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("failed to connect to user service: %v", err)
	}
	defer userConn.Close()
	handler.SetUserClient(userv1.NewUserServiceClient(userConn))

	messageDialCtx, cancelMessage := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelMessage()
	messageConn, err := grpc.DialContext(
		messageDialCtx,
		cfg.MessageAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		log.Fatalf("failed to connect to message service: %v", err)
	}
	defer messageConn.Close()
	handler.SetMessageClient(messagev1.NewMessageServiceClient(messageConn))

	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery())
	handler.RegisterRoutes(r)

	addr := ":" + cfg.Port

	log.Printf("gateway started on %s (env=%s)", addr, cfg.Env)

	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatal(err)
	}

	if err := r.Run(addr); err != nil {
		log.Fatal(err)
	}
}
