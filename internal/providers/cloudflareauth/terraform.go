package cloudflareauth

import "fmt"

func RenderTerraformBootstrap(plan *PermissionPlan) string {
	return fmt.Sprintf(`# Terraform bootstrap for Cloudflare permissions
# Requested services: %v
# Required scopes: %v
# TODO: emit concrete cloudflare_api_token resources once the permission
# mapping is finalized against Cloudflare's token permission groups.
`, plan.Services, plan.Scopes)
}
