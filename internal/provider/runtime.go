package provider

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/internal/shared"
)

type Launcher interface {
	Launch(executablePath string) (shared.CloudProvider, io.Closer, error)
}

type Session interface {
	Descriptor() Descriptor
	Provider() shared.CloudProvider
	Require(Capability) error
}

type runtimeSession struct {
	descriptor Descriptor
	provider   shared.CloudProvider
}

func (s *runtimeSession) Descriptor() Descriptor { return s.descriptor }

func (s *runtimeSession) Provider() shared.CloudProvider { return s.provider }

func (s *runtimeSession) Require(capability Capability) error {
	if !s.descriptor.Supports(capability) {
		return fmt.Errorf("provider %q does not support capability %q", s.descriptor.Name, capability)
	}
	return nil
}

type runtimeEntry struct {
	session Session
	closer  io.Closer
}

type Runtime struct {
	mu       sync.Mutex
	registry *Registry
	launcher Launcher
	warnings io.Writer
	sessions map[string]runtimeEntry
	warned   map[string]bool
	closed   bool
}

func NewRuntime(registry *Registry, launcher Launcher, warnings io.Writer) *Runtime {
	if warnings == nil {
		warnings = io.Discard
	}
	return &Runtime{
		registry: registry,
		launcher: launcher,
		warnings: warnings,
		sessions: make(map[string]runtimeEntry),
		warned:   make(map[string]bool),
	}
}

func (r *Runtime) List() []Descriptor { return r.registry.List() }

func (r *Runtime) Open(ctx context.Context, name string, config map[string]string) (Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("provider runtime is closed")
	}
	descriptor, err := r.registry.Resolve(name)
	if err != nil {
		return nil, err
	}
	key := name + ":" + configurationFingerprint(config)
	if entry, ok := r.sessions[key]; ok {
		return entry.session, nil
	}
	if descriptor.Origin == OriginCustom && !r.warned[name] {
		fmt.Fprintf(r.warnings, "warning: launching unsigned custom provider %q from %s\n", name, descriptor.ExecutablePath)
		r.warned[name] = true
	}
	providerClient, closer, err := r.launcher.Launch(descriptor.ExecutablePath)
	if err != nil {
		return nil, fmt.Errorf("launch provider %q: %w", name, err)
	}
	response, err := providerClient.Initialize(ctx, &pb.InitializeRequest{
		Provider: name,
		Config:   cloneStringMap(config),
	})
	if err != nil {
		_ = closer.Close()
		return nil, fmt.Errorf("initialize provider %q: %w", name, err)
	}
	if response == nil || !response.Success {
		_ = closer.Close()
		detail := "provider returned an unsuccessful response"
		if response != nil && strings.TrimSpace(response.Error) != "" {
			detail = response.Error
		}
		return nil, fmt.Errorf("initialize provider %q: %s", name, detail)
	}
	session := &runtimeSession{descriptor: descriptor, provider: providerClient}
	r.sessions[key] = runtimeEntry{session: session, closer: closer}
	return session, nil
}

func (r *Runtime) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	var errors []string
	for key, entry := range r.sessions {
		if err := entry.closer.Close(); err != nil {
			errors = append(errors, fmt.Sprintf("%s: %v", key, err))
		}
	}
	r.sessions = nil
	if len(errors) > 0 {
		sort.Strings(errors)
		return fmt.Errorf("close provider runtime: %s", strings.Join(errors, "; "))
	}
	return nil
}

func configurationFingerprint(config map[string]string) string {
	keys := make([]string, 0, len(config))
	for key := range config {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = io.WriteString(hash, key)
		_, _ = hash.Write([]byte{0})
		_, _ = io.WriteString(hash, config[key])
		_, _ = hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil))
}

func cloneStringMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
