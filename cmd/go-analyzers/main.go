// Command go-analyzers runs all custom analyzers as a standalone tool.
//
// Usage:
//
//	go vet -vettool=$(which go-analyzers) ./...
package main

import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/albertocavalcante/go-analyzers/clampcheck"
	"github.com/albertocavalcante/go-analyzers/makecopy"
	"github.com/albertocavalcante/go-analyzers/searchmigrate"
)

func main() {
	multichecker.Main(
		makecopy.Analyzer,
		searchmigrate.Analyzer,
		clampcheck.Analyzer,
	)
}
