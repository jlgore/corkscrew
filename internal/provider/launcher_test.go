package provider

import (
	"context"
	"github.com/hashicorp/go-hclog"
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

// go-plugin's lifecycle logging is quiet by default and follows CORKSCREW_PLUGIN_LOG.
func TestPluginLoggerLevel(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  hclog.Level
	}{
		{"", hclog.Warn},
		{"nonsense", hclog.Warn},
		{"debug", hclog.Debug},
		{"TRACE", hclog.Trace},
	} {
		if got := pluginLogger(tt.value).GetLevel(); got != tt.want {
			t.Errorf("pluginLogger(%q) level = %s, want %s", tt.value, got, tt.want)
		}
	}
}

// Plain plugin stderr lines reach the user, including lines go-plugin writes in pieces;
// structured hclog lines don't, since the level-filtered logger already handles them.
func TestPlainStderrLinesDropsJSONLogLines(t *testing.T) {
	var out strings.Builder
	w := &plainStderrLines{out: &out}
	for _, piece := range []string{
		`{"@level":"debug","@message":"plugin address"}`, "\n",
		"cloudflare GET /accounts -> 200 OK in 5ms", "\n",
		"partial ", "line\n",
	} {
		if _, err := w.Write([]byte(piece)); err != nil {
			t.Fatal(err)
		}
	}
	want := "cloudflare GET /accounts -> 200 OK in 5ms\npartial line\n"
	if out.String() != want {
		t.Errorf("passed through %q, want %q", out.String(), want)
	}
}
