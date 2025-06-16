#!/bin/bash

# Corkscrew Enterprise App Deployment Script
# This script deploys the Corkscrew enterprise application with proper permissions

set -e

echo "🚀 Deploying Corkscrew Enterprise Application"
echo "=============================================="

# Configuration
APP_NAME="Corkscrew Cloud Scanner"
APP_DESCRIPTION="Enterprise application for cloud resource scanning and discovery"

# Get current context
TENANT_ID=$(az account show --query tenantId -o tsv)
USER_ID=$(az ad signed-in-user show --query id -o tsv)

echo "Tenant ID: $TENANT_ID"
echo "User ID: $USER_ID"

# Create app registration
echo "Creating app registration..."
APP_ID=$(az ad app create \
    --display-name "$APP_NAME" \
    --sign-in-audience "AzureADMyOrg" \
    --query appId -o tsv)

echo "Created app registration: $APP_ID"

# Create service principal
echo "Creating service principal..."
SP_ID=$(az ad sp create --id $APP_ID --query id -o tsv)

echo "Created service principal: $SP_ID"

# Create client secret
echo "Creating client secret..."
SECRET=$(az ad app credential reset --id $APP_ID --query password -o tsv)

echo "Client secret created (save this securely): $SECRET"

# Grant API permissions (requires admin consent)
echo "Configuring API permissions..."

# Microsoft Graph permissions
az ad app permission add --id $APP_ID --api 00000003-0000-0000-c000-000000000000 --api-permissions 7ab1d382-f21e-4acd-a863-ba3e13f7da61=Role

# Azure Service Management permissions  
az ad app permission add --id $APP_ID --api 797f4846-ba00-4fd7-ba43-dac1f8f63013 --api-permissions 41094075-9dad-400e-a0bd-54e686782033=Scope

echo "API permissions configured (admin consent required)"

# Assign roles at management group level
ROOT_MG=$(az account management-group list --query "[?properties.details.parent==null].name" -o tsv | head -1)

if [ -n "$ROOT_MG" ]; then
    echo "Assigning roles at management group level: $ROOT_MG"
    
    # Reader role
    az role assignment create \
        --assignee $SP_ID \
        --role "Reader" \
        --scope "/providers/Microsoft.Management/managementGroups/$ROOT_MG"
    
    # Security Reader role
    az role assignment create \
        --assignee $SP_ID \
        --role "Security Reader" \
        --scope "/providers/Microsoft.Management/managementGroups/$ROOT_MG"
    
    echo "Role assignments completed"
else
    echo "Warning: No root management group found, assigning at subscription level"
    
    CURRENT_SUB=$(az account show --query id -o tsv)
    
    az role assignment create \
        --assignee $SP_ID \
        --role "Reader" \
        --scope "/subscriptions/$CURRENT_SUB"
fi

echo ""
echo "✅ Corkscrew Enterprise App Deployment Complete!"
echo "================================================"
echo "Application ID: $APP_ID"
echo "Service Principal ID: $SP_ID"
echo "Tenant ID: $TENANT_ID"
echo "Client Secret: $SECRET"
echo ""
echo "⚠️  IMPORTANT: Save the client secret securely!"
echo "⚠️  Admin consent required for API permissions"
echo ""
echo "Use these credentials to configure Corkscrew:"
echo "export AZURE_CLIENT_ID=$APP_ID"
echo "export AZURE_CLIENT_SECRET=$SECRET"
echo "export AZURE_TENANT_ID=$TENANT_ID"
