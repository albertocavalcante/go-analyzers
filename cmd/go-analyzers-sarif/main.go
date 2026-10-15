// Command go-analyzers-sarif runs the analyzers and exports native diagnostics,
// related locations and suggested fixes as SARIF 2.1.0.
package main

import (
	"context"
	"os"

	"github.com/albertocavalcante/go-analyzers/internal/sarifcmd"
)

func main() { os.Exit(sarifcmd.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr)) }
