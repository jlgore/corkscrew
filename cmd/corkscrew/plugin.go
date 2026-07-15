package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	providerapp "github.com/jlgore/corkscrew/internal/app/providers"
	scanapp "github.com/jlgore/corkscrew/internal/app/scan"
	providercatalog "github.com/jlgore/corkscrew/pkg/providers"
)

func runPluginE(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: corkscrew plugin <list|build|status|install|groups>")
	}
	switch args[0] {
	case "list":
		return listPlugins()
	case "build":
		return buildPlugins(context.Background(), args[1:])
	case "status":
		return checkPluginStatus(args[1:])
	case "install":
		return installPlugin(args[1:])
	case "groups":
		listServiceGroups()
		return nil
	default:
		return fmt.Errorf("unknown plugin command: %s", args[0])
	}
}

func listPlugins() error {
	descriptors, err := providerapp.ListManaged(os.Stderr)
	if err != nil {
		return fmt.Errorf("load provider registry: %w", err)
	}

	fmt.Println("📦 Available Plugins:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Provider\tOrigin\tStatus\tVersion\tDescription")
	for _, descriptor := range descriptors {
		status := "Not Installed"
		if descriptor.Installed {
			status = "Installed"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", descriptor.Name, descriptor.Origin, status, descriptor.Version, descriptor.Description)
	}
	return w.Flush()
}

func buildPlugins(ctx context.Context, names []string) error {
	if len(names) == 0 {
		names = providercatalog.Names()
	}
	var failures []string
	for _, name := range names {
		result, err := providerapp.BuildAndInstall(ctx, providerapp.BuildRequest{Provider: name}, providerapp.BuildDependencies{Warnings: os.Stderr})
		if err != nil {
			failures = append(failures, err.Error())
			fmt.Fprintf(os.Stderr, "❌ %s: %v\n", name, err)
			continue
		}
		fmt.Printf("✅ Built and installed %s %s (%s)\n", result.Manifest.Name, result.Manifest.Version, result.Origin)
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d provider build(s) failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return nil
}

func checkPluginStatus(names []string) error {
	descriptors, err := providerapp.ListManaged(os.Stderr)
	if err != nil {
		return fmt.Errorf("load provider registry: %w", err)
	}
	if len(names) == 0 {
		names = providercatalog.Names()
	}
	for _, name := range names {
		found := false
		for _, descriptor := range descriptors {
			if descriptor.Name != name {
				continue
			}
			found = true
			fmt.Printf("%s: origin=%s installed=%t version=%s capabilities=%v\n",
				descriptor.Name, descriptor.Origin, descriptor.Installed, descriptor.Version, descriptor.Capabilities)
			break
		}
		if !found {
			return fmt.Errorf("provider %s is not registered", name)
		}
	}
	return nil
}

func installPlugin(args []string) error {
	if len(args) != 2 || args[0] != "--manifest" {
		return fmt.Errorf("usage: corkscrew plugin install --manifest <plugin.json|plugin.yaml>")
	}
	installed, err := providerapp.InstallCustomManifest(args[1], os.Stderr)
	if err != nil {
		return err
	}
	fmt.Printf("✅ Installed custom provider %s\n", installed.Manifest.Name)
	return nil
}

func listServiceGroups() {
	fmt.Println("📦 Available Service Groups:")
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "Group\tServices")
	groups := scanapp.ServiceGroups()
	for _, group := range scanapp.ServiceGroupNames() {
		fmt.Fprintf(w, "%s\t%s\n", group, strings.Join(groups[group], ", "))
	}
	_ = w.Flush()
}
