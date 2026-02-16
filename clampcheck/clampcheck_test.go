package clampcheck_test

import (
	"testing"

	"github.com/albertocavalcante/go-analyzers/clampcheck"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestClampCheck(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, clampcheck.Analyzer, "clamptest")
}
