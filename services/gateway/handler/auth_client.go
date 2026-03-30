package handler

import authv1 "messenger/services/gateway/internal/pb/proto/auth/v1"

var authClient authv1.AuthServiceClient

func SetAuthClient(client authv1.AuthServiceClient) {
	authClient = client
}
