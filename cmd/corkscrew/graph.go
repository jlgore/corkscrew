package main

import (
	"os"

	"github.com/jlgore/corkscrew/pkg/graphquery"
)

func runGraph(args []string) {
	os.Exit(graphquery.NewRunner(os.Stdout, os.Stderr).Run(args))
}
