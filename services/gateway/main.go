package main

import (
	"log"

	"messenger/services/gateway/config"
	"messenger/services/gateway/handler"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

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
