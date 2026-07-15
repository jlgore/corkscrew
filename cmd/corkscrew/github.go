package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"

	"github.com/jlgore/corkscrew/internal/providers/githubauth"
)

func runGitHub(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: corkscrew github <command>")
		fmt.Println("Commands: bootstrap-app")
		return
	}

	switch args[0] {
	case "bootstrap-app":
		if err := runGitHubBootstrapApp(args[1:]); err != nil {
			fmt.Printf("GitHub App bootstrap failed: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Printf("Unknown github command: %s\n", args[0])
		fmt.Println("Available commands: bootstrap-app")
	}
}

func runGitHubBootstrapApp(args []string) error {
	fs := flag.NewFlagSet("github bootstrap-app", flag.ContinueOnError)
	org := fs.String("org", os.Getenv("GITHUB_ORG"), "GitHub organization slug")
	name := fs.String("name", "Corkscrew Scanner", "GitHub App name")
	callbackPort := fs.Int("port", 8947, "Local callback port")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *org == "" {
		return fmt.Errorf("--org or GITHUB_ORG is required")
	}

	if err := checkGHAuth(); err != nil {
		return err
	}

	conversion, err := githubauth.BootstrapApp(context.Background(), githubauth.BootstrapRequest{
		Org:     *org,
		AppName: *name,
		Port:    *callbackPort,
		OpenURL: openURL,
		Notify:  func(message string) { fmt.Println(message) },
	})
	if err != nil {
		return err
	}
	if _, err := githubauth.Store(*org, conversion); err != nil {
		return err
	}

	fmt.Println("GitHub App created and stored locally.")
	fmt.Printf("Install the app for %s: %s/installations/new\n", *org, conversion.HTMLURL)
	fmt.Println("After installation, Corkscrew can resolve the installation automatically; set installation_id only for locked-down environments.")
	return nil
}

func checkGHAuth() error {
	cmd := exec.Command("gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh auth status failed; run gh auth login first")
	}
	return nil
}

func openURL(url string) error {
	if _, err := exec.LookPath("gh"); err == nil {
		return exec.Command("gh", "browse", url).Run()
	}
	if _, err := exec.LookPath("xdg-open"); err == nil {
		return exec.Command("xdg-open", url).Run()
	}
	return fmt.Errorf("no browser opener found")
}
