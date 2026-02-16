package sortmigrate_test

import (
	"testing"

	"github.com/albertocavalcante/go-analyzers/sortmigrate"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestSortMigrate(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, sortmigrate.Analyzer, "sorttest")
}
