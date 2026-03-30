package handler

import (
	"net/http"
	"strconv"

	messagev1 "messenger/services/gateway/internal/pb/proto/message/v1"

	"github.com/gin-gonic/gin"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type sendMessageRequest struct {
	ToUsername      string `json:"to_username" binding:"required,min=3,max=32"`
	Text            string `json:"text" binding:"required,min=1,max=4096"`
	ClientMessageID string `json:"client_message_id"`
}

func registerMessageRoutes(v1 *gin.RouterGroup) {
	messages := v1.Group("/messages")
	messages.Use(RequireAuth())
	{
		messages.POST("/send", sendMessage)
		messages.GET("/:userId", getHistory)
	}
}

func mapMessageError(c *gin.Context, err error) {
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

func sendMessage(c *gin.Context) {
	if messageClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "message_unavailable", "message": "message client is not configured"})
		return
	}

	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, err.Error())
		return
	}

	senderID := c.GetString("user_id")
	if senderID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": "user id missing in token"})
		return
	}

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := messageClient.SendMessage(ctx, &messagev1.SendMessageRequest{
		SenderId:        senderID,
		ToUsername:      req.ToUsername,
		Text:            req.Text,
		ClientMessageId: req.ClientMessageID,
	})
	if err != nil {
		mapMessageError(c, err)
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"status": "ok",
		"message": gin.H{
			"id":         resp.GetMessageId(),
			"created_at": resp.GetCreatedAt(),
		},
	})
}

func getHistory(c *gin.Context) {
	if messageClient == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "code": "message_unavailable", "message": "message client is not configured"})
		return
	}

	userID := c.GetString("user_id")
	peerID := c.Param("userId")
	if userID == "" || peerID == "" {
		badRequest(c, "user ids are required")
		return
	}

	limit := int32(50)
	if raw := c.Query("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = int32(n)
		}
	}
	cursor := c.Query("cursor")

	ctx, cancel := authCallContext(c)
	defer cancel()
	resp, err := messageClient.GetHistory(ctx, &messagev1.GetHistoryRequest{
		UserId:     userID,
		PeerUserId: peerID,
		Limit:      limit,
		Cursor:     cursor,
	})
	if err != nil {
		mapMessageError(c, err)
		return
	}

	items := make([]gin.H, 0, len(resp.GetMessages()))
	for _, m := range resp.GetMessages() {
		items = append(items, gin.H{
			"id":           m.GetId(),
			"sender_id":    m.GetSenderId(),
			"receiver_id":  m.GetReceiverId(),
			"text":         m.GetText(),
			"is_delivered": m.GetIsDelivered(),
			"created_at":   m.GetCreatedAt(),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"status":      "ok",
		"messages":    items,
		"next_cursor": resp.GetNextCursor(),
	})
}
