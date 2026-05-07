package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/jlgore/corkscrew/internal/client"
	pb "github.com/jlgore/corkscrew/internal/proto"
	"github.com/jlgore/corkscrew/pkg/plugins"
)

// runPlugin handles plugin management commands
func runPlugin(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew plugin <command>")
		fmt.Println("Commands: list, build, status, install")
		return
	}

	command := args[0]
	switch command {
	case "list":
		listPlugins()
	case "build":
		buildPlugins(args[1:])
	case "status":
		checkPluginStatus(args[1:])
	case "install":
		installPlugin(args[1:])
	case "groups":
		listServiceGroups()
	default:
		fmt.Printf("Unknown plugin command: %s\n", command)
		fmt.Println("Available commands: list, build, status, install, groups")
	}
}

func listPlugins() {
	pm := plugins.NewPluginManager()
	pluginList := pm.ListAvailablePlugins()

	fmt.Println("📦 Available Plugins:")
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Provider\tStatus\tVersion\tDescription")
	fmt.Fprintln(w, "--------\t------\t-------\t-----------")

	for provider, info := range pluginList {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			provider,
			pm.GetPluginStatus(provider),
			info.Version,
			info.Description)
	}
	w.Flush()

	fmt.Println()
	fmt.Println("💡 Commands:")
	fmt.Println("   corkscrew plugin build <provider>    - Build a specific plugin")
	fmt.Println("   corkscrew plugin status <provider>   - Check plugin status")
	fmt.Println("   corkscrew plugin groups              - Show service groups")
}

func buildPlugins(args []string) {
	pm := plugins.NewPluginManager()

	providers := []string{"aws", "azure", "gcp", "kubernetes"}
	if len(args) > 0 {
		providers = args
	}

	fmt.Println("🔨 Building plugins...")

	for _, provider := range providers {
		fmt.Printf("  Building %s plugin...", provider)

		if !pm.CanBuildPlugin(provider) {
			fmt.Printf(" ❌ No source available\n")
			continue
		}

		if err := pm.BuildPlugin(provider); err != nil {
			fmt.Printf(" ❌ Failed: %v\n", err)
		} else {
			fmt.Printf(" ✅ Done\n")
		}
	}
}

func checkPluginStatus(args []string) {
	providers := []string{"aws", "azure"}
	if len(args) > 0 {
		providers = args
	}

	fmt.Println("🔍 Checking plugin status...")
	fmt.Println()

	for _, providerName := range providers {
		pc, err := client.NewPluginClient(providerName)
		if err != nil {
			pm := plugins.NewPluginManager()
			status := pm.GetPluginStatus(providerName)
			fmt.Printf("%s %s: %s\n", getStatusIcon(status), providerName, status)
			continue
		}
		defer pc.Close()

		provider, err := pc.GetProvider()
		if err != nil {
			fmt.Printf("❌ %s: Failed to initialize - %v\n", providerName, err)
			continue
		}

		info, err := provider.GetProviderInfo(context.Background(), &pb.Empty{})
		if err != nil {
			fmt.Printf("❌ %s: Failed to get info - %v\n", providerName, err)
			continue
		}

		fmt.Printf("✅ %s: Ready\n", providerName)
		fmt.Printf("   Version: %s\n", info.Version)
		fmt.Printf("   Services: %d\n", len(info.SupportedServices))
	}
}

func getStatusIcon(status string) string {
	switch {
	case strings.Contains(status, "Installed"):
		return "✅"
	case strings.Contains(status, "Can Build"):
		return "🔨"
	default:
		return "❌"
	}
}

func installPlugin(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew plugin install <provider>")
		return
	}

	provider := args[0]
	pm := plugins.NewPluginManager()

	// Check if already installed
	if status := pm.GetPluginStatus(provider); strings.Contains(status, "Installed") {
		fmt.Printf("✅ %s plugin is already installed\n", provider)
		return
	}

	fmt.Printf("📦 Installing %s plugin...\n", provider)

	if !pm.CanBuildPlugin(provider) {
		fmt.Printf("❌ No source available for %s plugin\n", provider)
		fmt.Println("\n💡 Available plugins:")
		fmt.Println("   corkscrew plugin list")
		return
	}

	// Prompt user for confirmation
	built, err := pm.PromptBuildPlugin(provider)
	if err != nil {
		fmt.Printf("❌ Installation failed: %v\n", err)
		return
	}

	if !built {
		fmt.Println("❌ Installation cancelled")
		return
	}

	fmt.Printf("✅ Successfully installed %s plugin\n", provider)
}

func listServiceGroups() {
	fmt.Println("📦 Available Service Groups:")
	fmt.Println()

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Group\tServices")
	fmt.Fprintln(w, "-----\t--------")

	for group, services := range serviceGroups {
		fmt.Fprintf(w, "%s\t%s\n", group, strings.Join(services, ", "))
	}
	w.Flush()

	fmt.Println()
	fmt.Println("💡 Usage Examples:")
	fmt.Println("   corkscrew scan --provider aws --services compute,storage")
	fmt.Println("   corkscrew scan --provider aws --services common,monitoring")
	fmt.Println("   corkscrew scan --provider aws --services database,s3")
}
