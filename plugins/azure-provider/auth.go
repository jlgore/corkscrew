package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/jlgore/corkscrew/internal/secrets"
)

func resolveAzureCredential(ctx context.Context, configMap map[string]string, subscriptionID, tenantID string, reader secrets.Reader) (azcore.TokenCredential, string, string, string, error) {
	source, err := secrets.CredentialSourceFromConfig(configMap)
	if err != nil {
		return nil, "", "", "", err
	}
	if !source.Configured() {
		cred, err := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{TenantID: tenantID})
		return cred, subscriptionID, tenantID, "DefaultAzureCredential", err
	}

	credential, err := (&secrets.CredentialResolver{Reader: reader}).ResolveCredential(ctx, secrets.CredentialRequest{
		Source:      source,
		DefaultKind: secrets.KindAzureServicePrincipal,
	})
	if err != nil {
		if source.AllowFallback {
			cred, fallbackErr := azidentity.NewDefaultAzureCredential(&azidentity.DefaultAzureCredentialOptions{TenantID: tenantID})
			return cred, subscriptionID, tenantID, "DefaultAzureCredential:fallback", fallbackErr
		}
		return nil, "", "", "", fmt.Errorf("read Azure auth secret: %w", err)
	}

	if credential.SubscriptionID != "" {
		subscriptionID = credential.SubscriptionID
	}
	if credential.TenantID != "" {
		tenantID = credential.TenantID
	}
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(credential.ClientID) == "" || strings.TrimSpace(credential.ClientSecret) == "" {
		return nil, "", "", "", fmt.Errorf("Azure auth secret requires tenant_id, client_id, and client_secret")
	}

	cred, err := azidentity.NewClientSecretCredential(tenantID, credential.ClientID, credential.ClientSecret, nil)
	if err != nil {
		return nil, "", "", "", fmt.Errorf("create Azure client secret credential: %w", err)
	}
	return cred, subscriptionID, tenantID, credential.Source, nil
}
