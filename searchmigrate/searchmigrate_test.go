package searchmigrate_test

import (
	"testing"

	"github.com/albertocavalcante/go-analyzers/searchmigrate"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSearchMigrate(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, searchmigrate.Analyzer, "searchtest")
}
