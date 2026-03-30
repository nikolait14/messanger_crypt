package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
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

	c.JSON(http.StatusCreated, gin.H{
		"status": "ok",
		"user": gin.H{
			"id":       "mock-user-id",
			"username": req.Username,
			"phone":    maskPhone(req.Phone),
		},
	})
}

func login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"tokens": gin.H{
			"access_token":  "mock-access-token",
			"refresh_token": "mock-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		},
	})
}

func refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"tokens": gin.H{
			"access_token":  "mock-access-token",
			"refresh_token": "mock-new-refresh-token",
			"token_type":    "Bearer",
			"expires_in":    3600,
		},
	})
}

func logout(c *gin.Context) {
	var req logoutRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}
