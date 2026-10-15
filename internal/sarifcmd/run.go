// Package sarifcmd is the IO boundary for the native SARIF command. Analyzer
// execution remains delegated to the standard go/analysis checker.
package sarifcmd

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/albertocavalcante/go-analyzers/clampcheck"
	"github.com/albertocavalcante/go-analyzers/makecopy"
	"github.com/albertocavalcante/go-analyzers/searchmigrate"
	"github.com/albertocavalcante/go-analyzers/sortmigrate"
	sarif "github.com/albertocavalcante/sarif-go"
	"github.com/albertocavalcante/sarif-go/goanalysis"
	"golang.org/x/tools/go/analysis"
	"golang.org/x/tools/go/analysis/checker"
	"golang.org/x/tools/go/packages"
)

// Run writes one SARIF report. Returns 0 for a completed scan below the selected
// threshold, 1 for findings, and 2 for configuration or execution failures.
func Run(ctx context.Context, args []string, out, stderr io.Writer) int {
	fs := flag.NewFlagSet("go-analyzers-sarif", flag.ContinueOnError)
	fs.SetOutput(stderr)
	rootFlag := fs.String("root", ".", "module checkout root")
	failOn := fs.String("fail-on", "warning", "findings threshold: none, warning")
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if *failOn != "none" && *failOn != "warning" {
		fmt.Fprintln(stderr, "invalid --fail-on; use none or warning")
		return 2
	}
	root, err := filepath.Abs(*rootFlag)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	uri, err := sarif.RootURI(root)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	var snapshots sync.Map
	config := &packages.Config{Context: ctx, Dir: root, Mode: packages.LoadAllSyntax, Tests: true}
	config.ParseFile = func(files *token.FileSet, name string, content []byte) (*ast.File, error) {
		if content == nil {
			var err error
			content, err = os.ReadFile(name)
			if err != nil {
				return nil, err
			}
		}
		snapshots.Store(name, append([]byte(nil), content...))
		return parser.ParseFile(files, name, content, parser.ParseComments|parser.AllErrors)
	}
	pkgs, err := packages.Load(config, patterns...)
	var failures []string
	if err != nil {
		failures = append(failures, err.Error())
	}
	if len(pkgs) == 0 {
		failures = append(failures, "no packages matched")
	}
	for _, pkg := range pkgs {
		for _, err := range pkg.Errors {
			failures = append(failures, err.Error())
		}
	}
	analyzers := []*analysis.Analyzer{makecopy.Analyzer, searchmigrate.Analyzer, clampcheck.Analyzer, sortmigrate.Analyzer}
	tool := sarif.Tool{Name: "go-analyzers", RootURI: uri, InformationURI: "https://github.com/albertocavalcante/go-analyzers"}
	for _, a := range analyzers {
		tool.Rules = append(tool.Rules, sarif.Rule{ID: a.Name, Description: a.Doc, HelpURI: a.URL})
	}
	var findings []sarif.Finding
	if len(failures) == 0 {
		graph, err := checker.Analyze(analyzers, pkgs, nil)
		if err != nil {
			failures = append(failures, err.Error())
		} else {
			seen := map[string]bool{}
			for _, action := range graph.Roots {
				if action.Err != nil {
					failures = append(failures, action.String()+": "+action.Err.Error())
					continue
				}
				adapter := goanalysis.Adapter{Root: root, Files: action.Package.Fset, ReadFile: func(name string) ([]byte, error) {
					content, ok := snapshots.Load(name)
					if !ok {
						return nil, fmt.Errorf("missing source snapshot for %s", name)
					}
					return content.([]byte), nil
				}}
				for _, d := range action.Diagnostics {
					f, err := adapter.Finding(action.Analyzer.Name, "warning", d)
					if err != nil {
						failures = append(failures, err.Error())
						continue
					}
					key, err := json.Marshal(f)
					if err != nil {
						failures = append(failures, err.Error())
						continue
					}
					if !seen[string(key)] {
						findings = append(findings, f)
						seen[string(key)] = true
					}
				}
			}
		}
	}
	report, err := sarif.Build(tool, findings, failures)
	if err == nil {
		err = sarif.Write(out, report)
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if len(failures) > 0 {
		return 2
	}
	if *failOn == "warning" && len(findings) > 0 {
		return 1
	}
	return 0
}
