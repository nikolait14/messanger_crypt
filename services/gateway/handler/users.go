package handler

import (
	"net/http"

	userv1 "messenger/services/gateway/internal/pb/proto/user/v1"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func registerUserRoutes(v1 *gin.RouterGroup) {
	users := v1.Group("/users")
	users.Use(RequireAuth())
	{
		users.GET("/search", searchUsers)
		users.GET("/me", me)
		users.GET("/:id", getUserByID)
	}
}

func mapUserError(c *gin.Context, err error) {
	st, ok := status.FromError(err)
	if !ok {
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "code": "upstream_error", "message": "upstream service unavailable"})
		return
	}
	switch st.Code() {
	case codes.InvalidArgument:
		c.JSON(http.StatusBadRequest, gin.H{"status": "error", "code": "invalid_request", "message": st.Message()})
	case codes.NotFound:
		c.JSON(http.StatusNotFound, gin.H{"status": "error", "code": "not_found", "message": st.Message()})
	default:
		c.JSON(http.StatusBadGateway, gin.H{"status": "error", "code": "upstream_error", "message": st.Message()})
	}
}

func toUserJSON(u *userv1.UserResponse) gin.H {
	return gin.H{
		"id":         u.GetId(),
		"username":   u.GetUsername(),
		"phone":      maskPhone(u.GetPhone()),
		"created_at": u.GetCreatedAt(),
	}
}

func searchUsers(c *gin.Context) {
	if userClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "user_unavailable", "message": "user client is not configured"})
		return
	}
	username := c.Query("username")
	if username == "" {
		badRequest(c, "username is required")
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()

	resp, err := userClient.SearchUsers(ctx, &userv1.SearchUsersRequest{
		Username: username,
		Limit:    20,
	})
	if err != nil {
		mapUserError(c, err)
		return
	}

	users := make([]gin.H, 0, len(resp.GetUsers()))
	for _, u := range resp.GetUsers() {
		users = append(users, toUserJSON(u))
	}
	c.JSON(http.StatusOK, gin.H{"status": "ok", "users": users})
}

func me(c *gin.Context) {
	if userClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "user_unavailable", "message": "user client is not configured"})
		return
	}
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": "user id missing in token"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := userClient.GetUser(ctx, &userv1.GetUserRequest{UserId: userID})
	if err != nil {
		mapUserError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "user": toUserJSON(resp)})
}

func getUserByID(c *gin.Context) {
	if userClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "user_unavailable", "message": "user client is not configured"})
		return
	}
	id := c.Param("id")
	if id == "" {
		badRequest(c, "id is required")
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := userClient.GetUser(ctx, &userv1.GetUserRequest{UserId: id})
	if err != nil {
		mapUserError(c, err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "ok", "user": toUserJSON(resp)})
}
