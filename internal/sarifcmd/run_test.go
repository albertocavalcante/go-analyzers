package sarifcmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeSARIF(t *testing.T) {
	for _, mode := range []string{"finding", "clean", "broken"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			source := "package sample\nfunc clone(src []int) []int { dst := make([]int,len(src)); copy(dst,src); return dst }\n"
			if mode == "clean" {
				source = "package sample\n"
			}
			if mode == "broken" {
				source = "package sample\nvar x = missing\n"
			}
			for name, data := range map[string]string{"go.mod": "module example.com/sample\n\ngo 1.25\n", "sample.go": source} {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var out, stderr bytes.Buffer
			code := Run(context.Background(), []string{"--root=" + dir}, &out, &stderr)
			want := 1
			if mode == "clean" {
				want = 0
			}
			if mode == "broken" {
				want = 2
			}
			if code != want {
				t.Fatalf("exit %d: %s", code, stderr.String())
			}
			var report struct {
				Version string
				Runs    []struct {
					Results []struct {
						RuleID string `json:"ruleId"`
						Fixes  []json.RawMessage
					}
					Invocations []struct {
						Successful bool `json:"executionSuccessful"`
					}
				}
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.Version != "2.1.0" || len(report.Runs) != 1 {
				t.Fatal("invalid SARIF")
			}
			if mode == "finding" && (len(report.Runs[0].Results) != 1 || len(report.Runs[0].Results[0].Fixes) != 1) {
				t.Fatalf("lost native fix: %s", out.String())
			}
			if mode == "broken" && report.Runs[0].Invocations[0].Successful {
				t.Fatal("broken package reported as clean")
			}
		})
	}
}
