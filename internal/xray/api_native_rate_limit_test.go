package xray

import (
	"context"
	"net"
	"testing"

	"github.com/xtls/xray-core/app/proxyman/command"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

type rateLimitHandler struct {
	command.UnimplementedHandlerServiceServer
	request chan *command.AlterInboundRequest
}

func (s *rateLimitHandler) AlterInbound(_ context.Context, request *command.AlterInboundRequest) (*command.AlterInboundResponse, error) {
	s.request <- request
	return &command.AlterInboundResponse{}, nil
}

func TestXrayAPIAddUserSendsNativeRateLimits(t *testing.T) {
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	handler := &rateLimitHandler{request: make(chan *command.AlterInboundRequest, 1)}
	command.RegisterHandlerServiceServer(server, handler)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	ctx := context.Background()
	conn, err := grpc.NewClient("passthrough:///rate-limit-test",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := command.NewHandlerServiceClient(conn)
	api := &XrayAPI{HandlerServiceClient: &client}
	user := map[string]any{
		"email":                      "user@example.test",
		"id":                         "b831381d-6324-4d53-ad4f-8cda48b30811",
		"uplink_speed_limit":         uint64(250000),
		"downlink_speed_limit":       uint64(875000),
		"rate_limit_group":           "inbound:vless-in",
		"downlink_group_speed_limit": uint64(1500000),
	}
	if err := api.AddUser("vless", "vless-in", user); err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	var request *command.AlterInboundRequest
	select {
	case request = <-handler.request:
	case <-ctx.Done():
		t.Fatal("AlterInbound request was not received")
	}
	message, err := request.Operation.GetInstance()
	if err != nil {
		t.Fatalf("unpack operation: %v", err)
	}
	operation, ok := message.(*command.AddUserOperation)
	if !ok {
		t.Fatalf("operation = %T, want *command.AddUserOperation", message)
	}
	got := operation.User
	if got.Email != ScopedEmail("vless-in", "user@example.test") {
		t.Errorf("email = %q, want scoped user identity", got.Email)
	}
	if got.UplinkSpeedLimit != 250000 {
		t.Errorf("uplink limit = %d, want 250000", got.UplinkSpeedLimit)
	}
	if got.DownlinkSpeedLimit != 875000 || got.RateLimitGroup != "inbound:vless-in" || got.DownlinkGroupSpeedLimit != 1500000 {
		t.Errorf("protobuf native limits = (%d, %q, %d), want (875000, %q, 1500000)", got.DownlinkSpeedLimit, got.RateLimitGroup, got.DownlinkGroupSpeedLimit, "inbound:vless-in")
	}
	if got.Account == nil {
		t.Error("AddUser request has no protocol account")
	}
	if request.Tag != "vless-in" || got.Email == "" || got.GetDownlinkSpeedLimit() != 875000 || got.GetRateLimitGroup() != "inbound:vless-in" || got.GetDownlinkGroupSpeedLimit() != 1500000 {
		t.Fatal("AlterInbound request did not preserve protocol user and rate-limit fields")
	}
}
