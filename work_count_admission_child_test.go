package gotreesitter_test

import (
	"os"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
)

// TestWorkCountAdmissionChild is the ordinary, untagged admission endpoint.
// The outer harness compiles it from the same private source snapshot as the
// tagged diagnostic child, then starts one fresh process for one public parse.
func TestWorkCountAdmissionChild(t *testing.T) {
	if os.Getenv(workCountSourcePathEnv) == "" || os.Getenv(workCountResultPathEnv) == "" {
		t.Skip("diagnostic work-count admission child is not configured")
	}
	input, err := loadWorkCountChildInput()
	if err != nil {
		t.Fatal(err)
	}
	parser := gotreesitter.NewParser(input.lang)
	tree, parseErr := parser.Parse(input.source)
	if parseErr != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("parse: %v", parseErr)
	}
	defer tree.Release()
	result, err := authenticateWorkCountTree(input, tree)
	if err != nil {
		t.Fatal(err)
	}
	result.Engine = workCountAdmissionEngine
	if err := writeWorkCountChildResult(result); err != nil {
		t.Fatal(err)
	}
}
