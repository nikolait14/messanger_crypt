package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	messagev1 "messenger/services/gateway/internal/pb/proto/message/v1"
	userv1 "messenger/services/gateway/internal/pb/proto/user/v1"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type wsTicket struct {
	UserID    string
	ExpiresAt time.Time
}

type wsClient struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (c *wsClient) writeJSON(v interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.WriteJSON(v)
}

var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(_ *http.Request) bool { return true },
	}

	ticketMu sync.Mutex
	tickets  = map[string]wsTicket{}

	onlineMu sync.RWMutex
	online   = map[string]*wsClient{}
)

func registerWSRoutes(r *gin.Engine, v1 *gin.RouterGroup) {
	protectedAuth := v1.Group("/auth")
	protectedAuth.Use(RequireAuth())
	protectedAuth.POST("/ws-ticket", issueWSTicket)

	r.GET("/ws", connectWS)
}

func issueWSTicket(c *gin.Context) {
	userID := c.GetString("user_id")
	if userID == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "unauthorized", "message": "missing user context"})
		return
	}

	t := uuid.NewString()
	ticketMu.Lock()
	tickets[t] = wsTicket{
		UserID:    userID,
		ExpiresAt: time.Now().Add(60 * time.Second),
	}
	ticketMu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
		"ticket": t,
		"ttl":    60,
	})
}

type wsInbound struct {
	To   string `json:"to"`
	Text string `json:"text"`
}

func connectWS(c *gin.Context) {
	ticket := c.Query("ticket")
	if ticket == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "missing_ticket", "message": "ticket is required"})
		return
	}

	ticketMu.Lock()
	data, ok := tickets[ticket]
	if ok && time.Now().After(data.ExpiresAt) {
		delete(tickets, ticket)
		ok = false
	}
	if ok {
		delete(tickets, ticket)
	}
	ticketMu.Unlock()
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"status": "error", "code": "invalid_ticket", "message": "ticket is invalid or expired"})
		return
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	client := &wsClient{conn: conn}

	onlineMu.Lock()
	online[data.UserID] = client
	onlineMu.Unlock()

	defer func() {
		onlineMu.Lock()
		delete(online, data.UserID)
		onlineMu.Unlock()
		_ = conn.Close()
	}()

	for {
		var msg wsInbound
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if msg.To == "" || msg.Text == "" {
			_ = client.writeJSON(gin.H{"type": "error", "message": "to and text are required"})
			continue
		}
		if messageClient == nil {
			_ = client.writeJSON(gin.H{"type": "error", "message": "message service unavailable"})
			continue
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		resp, err := messageClient.SendMessage(ctx, &messagev1.SendMessageRequest{
			SenderId:   data.UserID,
			ToUsername: msg.To,
			Text:       msg.Text,
		})
		cancel()
		if err != nil {
			_ = client.writeJSON(gin.H{"type": "error", "message": err.Error()})
			continue
		}

		_ = client.writeJSON(gin.H{
			"type":       "sent",
			"message_id": resp.GetMessageId(),
			"created_at": resp.GetCreatedAt(),
		})

		deliverToOnlineRecipient(c, data.UserID, msg.To, msg.Text, resp.GetMessageId(), resp.GetCreatedAt())
	}
}

func deliverToOnlineRecipient(c *gin.Context, fromUserID, toUsername, text, messageID, createdAt string) {
	if userClient == nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	found, err := userClient.SearchUsers(ctx, &userv1.SearchUsersRequest{
		Username: toUsername,
		Limit:    1,
	})
	if err != nil || len(found.GetUsers()) == 0 {
		return
	}
	u := found.GetUsers()[0]
	if u.GetUsername() != toUsername {
		return
	}

	onlineMu.RLock()
	rcv, ok := online[u.GetId()]
	onlineMu.RUnlock()
	if !ok {
		return
	}

	_ = rcv.writeJSON(gin.H{
		"type":       "message",
		"message_id": messageID,
		"from_user":  fromUserID,
		"text":       text,
		"created_at": createdAt,
	})
}
