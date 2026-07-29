//go:build cgo && (treesitter_c_parity || treesitter_c_bench)

package cgoharness

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentable/gotreesitter/internal/benchfixtures"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestCOracleDeepDigestIterativeDeepAndWide(t *testing.T) {
	language, err := COracleLanguage("json")
	if err != nil {
		t.Fatal(err)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}
	fixtures := map[string]string{
		"deep": strings.Repeat("[", 4096) + "0" + strings.Repeat("]", 4096),
		"wide": "[" + strings.Repeat("0,", 20000) + "0]",
	}
	for name, source := range fixtures {
		t.Run(name, func(t *testing.T) {
			tree := parser.Parse([]byte(source), nil)
			if tree == nil || tree.RootNode() == nil || tree.RootNode().HasError() {
				if tree != nil {
					tree.Close()
				}
				t.Fatal("C oracle did not produce a complete regression tree")
			}
			defer tree.Close()
			bounded, err := cOracleDeepDigestWithin(tree, 10*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			unbounded, err := COracleDeepDigest(tree)
			if err != nil {
				t.Fatal(err)
			}
			if bounded != unbounded || len(bounded) != 64 {
				t.Fatalf("digest mismatch bounded=%q unbounded=%q", bounded, unbounded)
			}
		})
	}
}

func TestCOracleDeepDigestWallTimeout(t *testing.T) {
	language, err := COracleLanguage("json")
	if err != nil {
		t.Fatal(err)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}
	tree := parser.Parse([]byte("[0,1,2,3]"), nil)
	if tree == nil {
		t.Fatal("C oracle returned nil timeout fixture")
	}
	defer tree.Close()
	if _, err := cOracleDeepDigestWithin(tree, 0); !errors.Is(err, errCOracleDeepDigestTimeout) {
		t.Fatalf("digest timeout error=%v", err)
	}
}

func TestCOracleContractPreflight(t *testing.T) {
	identity, err := COracleIdentity("go")
	if err != nil {
		t.Fatal(err)
	}
	if identity.Contract != COracleContractVersion {
		t.Fatalf("contract=%q want %q", identity.Contract, COracleContractVersion)
	}
	if identity.BindingModule != COracleBindingModule || identity.BindingVersion != COracleBindingVersion {
		t.Fatalf("binding=%s@%s want %s@%s", identity.BindingModule, identity.BindingVersion, COracleBindingModule, COracleBindingVersion)
	}
	if identity.RuntimeCommit != COracleRuntimeCommit {
		t.Fatalf("runtime commit=%s want %s", identity.RuntimeCommit, COracleRuntimeCommit)
	}
	if identity.GrammarCommit != benchfixtures.GoGrammarCommit {
		t.Fatalf("Go grammar commit=%s want %s", identity.GrammarCommit, benchfixtures.GoGrammarCommit)
	}
	if !strings.Contains(identity.GrammarCompileFlags, "-O2") {
		t.Fatalf("grammar flags=%q do not include -O2", identity.GrammarCompileFlags)
	}
	if identity.RuntimeLinkage != "static_cgo_test_binary" || identity.GrammarLinkage != "shared_dlopen" {
		t.Fatalf("unexpected cgo transport linkage: runtime=%s grammar=%s", identity.RuntimeLinkage, identity.GrammarLinkage)
	}
	if len(identity.GrammarArtifactSHA256) != 64 {
		t.Fatalf("grammar artifact sha256=%q", identity.GrammarArtifactSHA256)
	}

	versionOut, err := exec.Command("go", "list", "-m", "-f", "{{.Version}}", COracleBindingModule).Output()
	if err != nil {
		t.Fatalf("resolve C oracle binding module: %v", err)
	}
	if version := strings.TrimSpace(string(versionOut)); version != COracleBindingVersion {
		t.Fatalf("resolved binding version=%s want %s", version, COracleBindingVersion)
	}

	encoded, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("c_oracle_identity=%s", encoded)

	parser := sitter.NewParser()
	defer parser.Close()
	language, err := COracleLanguage("go")
	if err != nil {
		t.Fatal(err)
	}
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}
	source := makeGoBenchmarkSource(500)
	tree := parser.Parse(source, nil)
	if tree == nil {
		t.Fatal("C oracle preflight parse returned nil")
	}
	defer tree.Close()
	root := tree.RootNode()
	if root == nil || root.HasError() || root.EndByte() != uint(len(source)) {
		t.Fatalf("C oracle preflight parse incomplete: root=%v", root)
	}
	digest, err := COracleDeepDigest(tree)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("c_oracle_deep_digest_format=gts-deep-tree-v1 c_oracle_deep_sha256=%s", digest)
}

func TestCOracleStaticDeepParity(t *testing.T) {
	identity, err := COracleIdentity("go")
	if err != nil {
		t.Fatal(err)
	}
	language, err := COracleLanguage("go")
	if err != nil {
		t.Fatal(err)
	}
	parser := sitter.NewParser()
	defer parser.Close()
	if err := parser.SetLanguage(language); err != nil {
		t.Fatal(err)
	}

	fixtures, err := benchfixtures.LoadGoFullParseFixtures()
	if err != nil {
		t.Fatal(err)
	}
	suiteCoverage := make(benchfixtures.NodeKindCoverage)
	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.Fixture.ID, func(t *testing.T) {
			tree := parser.Parse(fixture.Source, nil)
			if tree == nil {
				t.Fatal("cgo C oracle returned nil tree")
			}
			root := tree.RootNode()
			if root == nil || root.HasError() || root.EndByte() != uint(len(fixture.Source)) {
				tree.Close()
				t.Fatalf("cgo C oracle incomplete: root=%v", root)
			}
			coverage := cOracleNodeKindCoverage(root)
			if err := fixture.Fixture.VerifyNodeKindCoverage(coverage); err != nil {
				tree.Close()
				t.Fatalf("cgo C oracle workload identity: %v", err)
			}
			suiteCoverage.Merge(coverage)
			digest, err := COracleDeepDigest(tree)
			tree.Close()
			if err != nil {
				t.Fatal(err)
			}
			if err := fixture.Fixture.VerifyDeepTreeDigest(digest); err != nil {
				t.Fatalf("cgo C oracle: %v", err)
			}

			sourcePath := filepath.Join(t.TempDir(), fixture.Fixture.ID+".go")
			if err := os.WriteFile(sourcePath, fixture.Source, 0o600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("bash", "pure_c/run_go_benchmark.sh", "500", "1", "1", sourcePath)
			cmd.Env = append(os.Environ(), "GTS_C_ORACLE_EXPECTED_DEEP_SHA256="+digest)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("static C oracle: %v\n%s", err, out)
			}
			if !strings.Contains(string(out), "oracle_deep_sha256="+digest+"\n") {
				t.Fatalf("static C oracle did not report expected digest %s\n%s", digest, out)
			}
			for _, required := range []string{
				"oracle_contract=" + identity.Contract,
				"oracle_transport=static_executable",
				"oracle_linkage=runtime_and_grammar_statically_linked",
				"oracle_binding_commit=" + identity.BindingCommit,
				"oracle_runtime_commit=" + identity.RuntimeCommit,
				"oracle_runtime_checkout_clean=true",
				"oracle_grammar_commit=" + identity.GrammarCommit,
				"oracle_grammar_checkout_clean=true",
				"oracle_source_mode=immutable_snapshot",
				"oracle_source_sha256=" + fixture.Fixture.SHA256,
			} {
				if !strings.Contains(string(out), required+"\n") {
					t.Fatalf("static C oracle output missing %q\n%s", required, out)
				}
			}
			artifactSHA := oracleOutputValue(string(out), "oracle_artifact_sha256")
			if len(artifactSHA) != 64 {
				t.Fatalf("static C oracle artifact sha256=%q\n%s", artifactSHA, out)
			}
			buildKey := oracleOutputValue(string(out), "oracle_build_key_sha256")
			if len(buildKey) != 64 {
				t.Fatalf("static C oracle build key sha256=%q\n%s", buildKey, out)
			}
			for _, key := range []string{"oracle_runtime_lib_src_tree_oid", "oracle_grammar_src_tree_oid"} {
				if oid := oracleOutputValue(string(out), key); len(oid) != 40 && len(oid) != 64 {
					t.Fatalf("static C oracle %s=%q is not a Git object ID\n%s", key, oid, out)
				}
			}
			t.Logf("source_sha256=%s deep_sha256=%s artifact_sha256=%s", fixture.Fixture.SHA256, digest, artifactSHA)
		})
	}
	if err := benchfixtures.VerifyGoFullParseSuiteNodeKindCoverage(suiteCoverage); err != nil {
		t.Fatalf("cgo/static C oracle workload identity: %v", err)
	}
}

func cOracleNodeKindCoverage(root *sitter.Node) benchfixtures.NodeKindCoverage {
	coverage := make(benchfixtures.NodeKindCoverage)
	var walk func(*sitter.Node)
	walk = func(node *sitter.Node) {
		if node == nil {
			return
		}
		coverage[node.Kind()]++
		for i := uint(0); i < node.ChildCount(); i++ {
			walk(node.Child(i))
		}
	}
	walk(root)
	return coverage
}

func oracleOutputValue(output, key string) string {
	prefix := key + "="
	for line := range strings.SplitSeq(output, "\n") {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix)
		}
	}
	return ""
}
