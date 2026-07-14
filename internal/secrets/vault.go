package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
)

const (
	ProviderVault = "vault"

	EngineKVV1 = "kv-v1"
	EngineKVV2 = "kv-v2"
)

type Reference struct {
	Provider  string
	Engine    string
	Address   string
	Token     string
	TokenEnv  string
	Namespace string
	Mount     string
	Path      string
	Version   int
}

type Reader interface {
	ReadSecret(ctx context.Context, ref Reference) (map[string]string, error)
}

type VaultReader struct {
	HTTPClient *http.Client
}

func (r *VaultReader) ReadSecret(ctx context.Context, ref Reference) (map[string]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if provider := strings.TrimSpace(ref.Provider); provider != "" && provider != ProviderVault {
		return nil, fmt.Errorf("unsupported secret provider %q", provider)
	}

	address := firstNonEmpty(ref.Address, os.Getenv("VAULT_ADDR"))
	if address == "" {
		return nil, fmt.Errorf("vault address not configured")
	}

	token := firstNonEmpty(ref.Token, envValue(ref.TokenEnv), os.Getenv("VAULT_TOKEN"))
	if token == "" {
		return nil, fmt.Errorf("vault token not configured")
	}

	secretURL, engine, err := vaultSecretURL(address, ref)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, secretURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Vault-Token", token)
	if ref.Namespace != "" {
		req.Header.Set("X-Vault-Namespace", ref.Namespace)
	}

	client := http.DefaultClient
	if r != nil && r.HTTPClient != nil {
		client = r.HTTPClient
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("read vault secret: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read vault response: %w", err)
	}

	var payload vaultSecretResponse
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode vault response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if len(payload.Errors) > 0 {
			return nil, fmt.Errorf("read vault secret: %s", strings.Join(payload.Errors, "; "))
		}
		return nil, fmt.Errorf("read vault secret: status %d", resp.StatusCode)
	}

	data := payload.Data
	if engine == EngineKVV2 {
		nested, ok := payload.Data["data"].(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("vault kv-v2 response missing data.data")
		}
		data = nested
	}
	return stringifySecretData(data), nil
}

type vaultSecretResponse struct {
	Data   map[string]interface{} `json:"data"`
	Errors []string               `json:"errors"`
}

func vaultSecretURL(address string, ref Reference) (string, string, error) {
	engine, err := normalizeEngine(ref.Engine)
	if err != nil {
		return "", "", err
	}

	secretPath := cleanPath(ref.Path)
	if secretPath == "" {
		return "", "", fmt.Errorf("vault secret path not configured")
	}
	mount := cleanPath(ref.Mount)

	var logicalPath string
	switch engine {
	case EngineKVV2:
		if mount == "" {
			if !strings.Contains(secretPath, "/data/") {
				return "", "", fmt.Errorf("vault kv-v2 secret requires mount unless path already contains /data/")
			}
			logicalPath = secretPath
		} else {
			logicalPath = path.Join(mount, "data", secretPath)
		}
	case EngineKVV1:
		if mount == "" {
			logicalPath = secretPath
		} else {
			logicalPath = path.Join(mount, secretPath)
		}
	}

	if !strings.Contains(address, "://") {
		address = "https://" + address
	}
	parsed, err := url.Parse(address)
	if err != nil {
		return "", "", fmt.Errorf("parse vault address: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", "", fmt.Errorf("invalid vault address %q", address)
	}

	parsed.Path = joinURLPath(parsed.Path, "v1", logicalPath)
	if ref.Version > 0 {
		query := parsed.Query()
		query.Set("version", strconv.Itoa(ref.Version))
		parsed.RawQuery = query.Encode()
	}
	return parsed.String(), engine, nil
}

func normalizeEngine(engine string) (string, error) {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(engine), "_", "-")) {
	case "", "kv", "kv2", EngineKVV2:
		return EngineKVV2, nil
	case "kv1", EngineKVV1, "generic":
		return EngineKVV1, nil
	default:
		return "", fmt.Errorf("unsupported vault secret engine %q", engine)
	}
}

func stringifySecretData(data map[string]interface{}) map[string]string {
	out := make(map[string]string, len(data))
	for key, value := range data {
		if str, ok := valueAsString(value); ok {
			out[key] = str
		}
	}
	return out
}

func valueAsString(value interface{}) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", false
	case string:
		return v, true
	case bool:
		return strconv.FormatBool(v), true
	case json.Number:
		return v.String(), true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	case []interface{}:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			part, ok := valueAsString(item)
			if !ok {
				continue
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, ","), true
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v), true
		}
		return string(encoded), true
	}
}

func cleanPath(value string) string {
	return strings.Trim(strings.TrimSpace(value), "/")
}

func joinURLPath(base string, parts ...string) string {
	all := make([]string, 0, len(parts)+1)
	if cleaned := cleanPath(base); cleaned != "" {
		all = append(all, cleaned)
	}
	for _, part := range parts {
		if cleaned := cleanPath(part); cleaned != "" {
			all = append(all, cleaned)
		}
	}
	if len(all) == 0 {
		return "/"
	}
	return "/" + path.Join(all...)
}

func envValue(name string) string {
	if strings.TrimSpace(name) == "" {
		return ""
	}
	return os.Getenv(name)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
