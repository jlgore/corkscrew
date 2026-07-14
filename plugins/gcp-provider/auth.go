package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jlgore/corkscrew/internal/secrets"
	"golang.org/x/oauth2/google"
)

func resolveGCPCredentials(ctx context.Context, configMap map[string]string, reader secrets.Reader) (*google.Credentials, string, error) {
	source, err := secrets.CredentialSourceFromConfig(configMap)
	if err != nil {
		return nil, "", err
	}
	if !source.Configured() {
		creds, err := google.FindDefaultCredentials(ctx, gcpOAuthScopes...)
		return creds, "ApplicationDefaultCredentials", err
	}

	credential, err := (&secrets.CredentialResolver{Reader: reader}).ResolveCredential(ctx, secrets.CredentialRequest{
		Source:      source,
		DefaultKind: secrets.KindGCPServiceAccount,
	})
	if err != nil {
		if source.AllowFallback {
			creds, fallbackErr := google.FindDefaultCredentials(ctx, gcpOAuthScopes...)
			return creds, "ApplicationDefaultCredentials:fallback", fallbackErr
		}
		return nil, "", fmt.Errorf("read GCP auth secret: %w", err)
	}
	if credential.Kind != secrets.KindGCPServiceAccount {
		return nil, "", fmt.Errorf("unsupported GCP secret credential kind %q", credential.Kind)
	}

	material := credential.ServiceAccountJSON
	if material == "" {
		var buildErr error
		material, buildErr = serviceAccountJSONFromFields(credential)
		if buildErr != nil {
			return nil, "", buildErr
		}
	}

	creds, err := google.CredentialsFromJSON(ctx, []byte(material), gcpOAuthScopes...)
	if err != nil {
		return nil, "", fmt.Errorf("parse GCP service account credentials: %w", err)
	}
	return creds, credential.Source, nil
}

func serviceAccountJSONFromFields(credential *secrets.Credential) (string, error) {
	if credential.ProjectID == "" || credential.ClientEmail == "" || credential.PrivateKey == "" {
		return "", fmt.Errorf("GCP auth secret requires service_account_json or project_id, client_email, and private_key")
	}
	payload := map[string]string{
		"type":         "service_account",
		"project_id":   credential.ProjectID,
		"private_key":  credential.PrivateKey,
		"client_email": credential.ClientEmail,
		"token_uri":    "https://oauth2.googleapis.com/token",
	}
	if credential.ClientID != "" {
		payload["client_id"] = credential.ClientID
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode GCP service account credentials: %w", err)
	}
	return string(encoded), nil
}
