package handler

import userv1 "messenger/services/gateway/internal/pb/proto/user/v1"

var userClient userv1.UserServiceClient

func SetUserClient(client userv1.UserServiceClient) {
	userClient = client
}
