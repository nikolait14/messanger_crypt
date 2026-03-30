package handler

import (
	"context"
	"net/http"
	"time"

	authv1 "messenger/services/gateway/internal/pb/proto/auth/v1"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type registerRequest struct {
	Username string `json:"username" binding:"required,min=3,max=32"`
	Phone    string `json:"phone" binding:"required,min=10,max=20"`
	Password string `json:"password" binding:"required,min=8,max=72"`
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

func registerAuthRoutes(v1 *gin.RouterGroup) {
	auth := v1.Group("/auth")
	{
		auth.POST("/register", register)
		auth.POST("/login", login)
		auth.POST("/refresh", refresh)
		auth.POST("/logout", logout)
	}
}

func badRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{
		"status":  "error",
		"code":    "invalid_request",
		"message": message,
	})
}

func upstreamError(c *gin.Context, err error) {
	st, ok := status.FromError(err)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "code": "upstream_error", "message": "upstream service unavailable"})
		return
	}

	switch st.Code() {
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "code": "invalid_request", "message": st.Message()})
	case codes.AlreadyExists:
		c.JSON(http.StatusConflict, gin.H{"status": "error", "code": "already_exists", "message": st.Message()})
	case codes.Unauthenticated:
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": st.Message()})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "code": "upstream_error", "message": st.Message()})
	}
}

func authCallContext(c *gin.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.Request.Context(), 3*time.Second)
}

func maskPhone(phone string) string {
	if len(phone) <= 4 {
		return phone
	}

	return "***" + phone[len(phone)-4:]
}
func register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	if authClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "auth_unavailable", "message": "auth client is not configured"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := authClient.Register(ctx, &authv1.RegisterRequest{
		Username: req.Username,
		Phone:    req.Phone,
		Password: req.Password,
	})
	if err != nil {
		upstreamError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "ok",
		"user": gin.H{
			"id":       resp.GetUserId(),
			"username": resp.GetUsername(),
			"phone":    maskPhone(resp.GetPhone()),
		},
	})
}

func login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	if authClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "auth_unavailable", "message": "auth client is not configured"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := authClient.Login(ctx, &authv1.LoginRequest{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		upstreamError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"tokens": gin.H{
			"access_token":  resp.GetAccessToken(),
			"refresh_token": resp.GetRefreshToken(),
			"token_type":    "Bearer",
			"expires_in":    resp.GetExpiresIn(),
		},
	})
}

func refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	if authClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "auth_unavailable", "message": "auth client is not configured"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := authClient.Refresh(ctx, &authv1.RefreshRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		upstreamError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"tokens": gin.H{
			"access_token":  resp.GetAccessToken(),
			"refresh_token": resp.GetRefreshToken(),
			"token_type":    "Bearer",
			"expires_in":    resp.GetExpiresIn(),
		},
	})
}

func logout(c *gin.Context) {
	var req logoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	if authClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "auth_unavailable", "message": "auth client is not configured"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := authClient.Logout(ctx, &authv1.LogoutRequest{
		RefreshToken: req.RefreshToken,
	})
	if err != nil {
		upstreamError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "ok",
		"success": resp.GetSuccess(),
	})
}
