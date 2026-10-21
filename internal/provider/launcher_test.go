package provider

import (
	"context"
	"net"
	"os/exec"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

func TestPluginClientConfigSetsMessageLimit(t *testing.T) {
	config := pluginClientConfig(exec.Command("true"))
	if len(config.GRPCDialOptions) == 0 {
		t.Fatal("plugin client config has no gRPC dial options; go-plugin's MaxInt32 applies")
	}
	if MaxPluginMessageBytes <= 4<<20 || MaxPluginMessageBytes >= 1<<31-1 {
		t.Fatalf("MaxPluginMessageBytes = %d, want above gRPC's 4 MiB and below MaxInt32",
			MaxPluginMessageBytes)
	}
}

// TestPluginDialOptionsEnforceLimit dials a real gRPC server with the options builder and a
// tiny limit, so a reply over it must fail with the explained error rather than be accepted.
func TestPluginDialOptionsEnforceLimit(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	healthpb.RegisterHealthServer(server, health.NewServer())
	go func() { _ = server.Serve(listener) }()
	defer server.Stop()

	dial := func(limitBytes int) healthpb.HealthClient {
		options := append([]grpc.DialOption{
			grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
				return listener.DialContext(ctx)
			}),
			grpc.WithTransportCredentials(insecure.NewCredentials()),
		}, pluginDialOptions(limitBytes)...)
		conn, err := grpc.NewClient("passthrough:///bufnet", options...)
		if err != nil {
			t.Fatalf("dial: %v", err)
		}
		t.Cleanup(func() { _ = conn.Close() })
		return healthpb.NewHealthClient(conn)
	}

	ctx := context.Background()
	if _, err := dial(MaxPluginMessageBytes).Check(ctx, &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("check under the limit: %v", err)
	}
	// A SERVING health reply is two bytes on the wire, so a one-byte limit rejects it.
	_, err := dial(1).Check(ctx, &healthpb.HealthCheckRequest{})
	if err == nil || !strings.Contains(err.Error(), "1-byte message limit") {
		t.Fatalf("check over the limit error = %v, want explained limit error", err)
	}
}
