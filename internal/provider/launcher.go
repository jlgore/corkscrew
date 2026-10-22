package provider

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/jlgore/corkscrew/internal/shared"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MaxPluginMessageBytes caps one gRPC message between corkscrew and a provider plugin.
// go-plugin defaults both directions to math.MaxInt32 (2 GiB), so a runaway plugin could make
// the CLI buffer gigabytes for a single reply. A BatchScan reply carries one scope's resources;
// 256 MiB holds several hundred thousand typical resource records, well above real scopes.
const MaxPluginMessageBytes = 256 << 20

type HashicorpLauncher struct{}

func (HashicorpLauncher) Launch(executablePath string) (shared.CloudProvider, io.Closer, error) {
	client := plugin.NewClient(pluginClientConfig(exec.Command(executablePath)))
	rpcClient, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("create plugin RPC client: %w", err)
	}
	raw, err := rpcClient.Dispense("provider")
	if err != nil {
		client.Kill()
		return nil, nil, fmt.Errorf("dispense provider plugin: %w", err)
	}
	providerClient, ok := raw.(shared.CloudProvider)
	if !ok {
		client.Kill()
		return nil, nil, fmt.Errorf("provider plugin returned unexpected type")
	}
	return providerClient, pluginProcess{client: client}, nil
}

// pluginClientConfig builds the go-plugin client config for one provider process.
func pluginClientConfig(cmd *exec.Cmd) *plugin.ClientConfig {
	return &plugin.ClientConfig{
		HandshakeConfig:  shared.HandshakeConfig,
		Plugins:          shared.PluginMap,
		Cmd:              cmd,
		AllowedProtocols: []plugin.Protocol{plugin.ProtocolGRPC},
		SyncStdout:       os.Stdout,
		SyncStderr:       os.Stderr,
		// Plain lines a plugin writes to stderr (its own errors, CORKSCREW_CLOUDFLARE_TRACE
		// output) reach the user; go-plugin otherwise only logs them at debug level.
		Stderr:          &plainStderrLines{out: os.Stderr},
		Logger:          pluginLogger(os.Getenv(pluginLogEnv)),
		GRPCDialOptions: pluginDialOptions(MaxPluginMessageBytes),
	}
}

// plainStderrLines passes a plugin's plain stderr lines through and drops its hclog JSON lines.
// go-plugin writes every stderr line to ClientConfig.Stderr before applying the log level, and
// the JSON lines already reach the user through Logger, filtered by level. go-plugin writes a line
// and its newline separately, so lines are buffered until complete.
type plainStderrLines struct {
	out io.Writer
	buf []byte
}

func (w *plainStderrLines) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		end := bytes.IndexByte(w.buf, '\n')
		if end < 0 {
			return len(p), nil
		}
		line := w.buf[:end+1]
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte(`{"@`)) {
			if _, err := w.out.Write(line); err != nil {
				return len(p), err
			}
		}
		w.buf = w.buf[end+1:]
	}
}

// pluginLogEnv sets the level of go-plugin's own logging (plugin start, RPC address, exit).
const pluginLogEnv = "CORKSCREW_PLUGIN_LOG"

// pluginLogger logs go-plugin's lifecycle at level, warn by default. go-plugin's fallback logger
// is at trace, which printed plugin start-up details on every scan.
func pluginLogger(level string) hclog.Logger {
	parsed := hclog.LevelFromString(level)
	if parsed == hclog.NoLevel {
		parsed = hclog.Warn
	}
	return hclog.New(&hclog.LoggerOptions{Name: "plugin", Level: parsed, Output: os.Stderr})
}

// pluginDialOptions replaces go-plugin's MaxInt32 message limits. go-plugin appends these
// after its own defaults, and gRPC applies call options in order, so these take effect.
func pluginDialOptions(limitBytes int) []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithDefaultCallOptions(
			grpc.MaxCallRecvMsgSize(limitBytes),
			grpc.MaxCallSendMsgSize(limitBytes),
		),
		grpc.WithChainUnaryInterceptor(explainMessageLimit(limitBytes)),
	}
}

// explainMessageLimit rewrites gRPC's terse ResourceExhausted error so a user whose scan
// exceeds the message limit learns which limit it was and how to stay under it.
func explainMessageLimit(limitBytes int) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		err := invoker(ctx, method, req, reply, cc, opts...)
		if status.Code(err) != codes.ResourceExhausted {
			return err
		}
		return fmt.Errorf("provider plugin call %s exceeded the %d-byte message limit; "+
			"scan fewer services or regions per run: %w", method, limitBytes, err)
	}
}

type pluginProcess struct{ client *plugin.Client }

func (p pluginProcess) Close() error {
	p.client.Kill()
	return nil
}
