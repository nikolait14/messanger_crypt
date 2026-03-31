package config

import "os"

type Config struct {
	Port         string
	Env          string
	AuthGRPCAddr string
	UserGRPCAddr string
	MessageAddr  string
	JWTSecret    string
}

func Load() Config {
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = getenv("PORT", "8080")
	}

	return Config{
		Port:         port,
		Env:          getenv("APP_ENV", "local"),
		AuthGRPCAddr: getenv("AUTH_GRPC_ADDR", "auth:9001"),
		UserGRPCAddr: getenv("USER_GRPC_ADDR", "user:9002"),
		MessageAddr:  getenv("MESSAGE_GRPC_ADDR", "message:9003"),
		JWTSecret:    getenv("JWT_ACCESS_SECRET", "change_me_access_secret"),
	}
}

func getenv(key, fallback string) string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	return v
}
