package makecopy_test

import (
	"testing"

	"github.com/albertocavalcante/go-analyzers/makecopy"
	"golang.org/x/tools/go/analysis/analysistest"
)

func TestMakeCopy(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, makecopy.Analyzer, "makecopytest")
}
