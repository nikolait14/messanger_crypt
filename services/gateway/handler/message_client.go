package handler

import messagev1 "messenger/services/gateway/internal/pb/proto/message/v1"

var messageClient messagev1.MessageServiceClient

func SetMessageClient(client messagev1.MessageServiceClient) {
	messageClient = client
}
