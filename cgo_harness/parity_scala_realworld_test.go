//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"

	gotreesitter "github.com/agentable/gotreesitter"
)

const (
	scalaRealWorldRepoURL = "https://github.com/scala/scala.git"
	// Pinned for reproducible corpus parity.
	scalaRealWorldCommit = "9ca90550490028efc7a75ebb6ccac51b599d6689"
)

type scalaRealWorldCase struct {
	name string
	path string
}

var scalaRealWorldCases = []scalaRealWorldCase{
	{name: "small-tailrec", path: "src/library/scala/annotation/tailrec.scala"},
	{name: "medium-try", path: "src/library/scala/util/Try.scala"},
	{name: "large-list", path: "src/library/scala/collection/immutable/List.scala"},
	{name: "xlarge-future", path: "src/library/scala/concurrent/Future.scala"},
}

type scalaRealWorldExpectation struct {
	// maxNodeDivergences is a regression ratchet ceiling.
	// Strict mode requires 0 and ignores this budget.
	maxNodeDivergences int
	// allowGoHasError gates current known parser-error shape for large files.
	allowGoHasError bool
}

// Ratchet baselines captured with:
// GOT_PARSE_NODE_LIMIT_SCALE=3 GOT_GLR_MAX_STACKS=8
var scalaRealWorldExpectations = map[string]scalaRealWorldExpectation{
	"small-tailrec": {maxNodeDivergences: 0, allowGoHasError: false},
	"medium-try":    {maxNodeDivergences: 13, allowGoHasError: false},
	"large-list":    {maxNodeDivergences: 1, allowGoHasError: true},
	"xlarge-future": {maxNodeDivergences: 1, allowGoHasError: true},
}

func TestParityScalaRealWorldCorpus(t *testing.T) {
	if parityLanguageExcluded("scala") {
		t.Skip("scala excluded by GTS_PARITY_SKIP_LANGS")
	}
	// Keep this suite deterministic and non-truncating by default.
	if _, ok := os.LookupEnv("GOT_PARSE_NODE_LIMIT_SCALE"); !ok {
		t.Setenv("GOT_PARSE_NODE_LIMIT_SCALE", "3")
	}
	if _, ok := os.LookupEnv("GOT_GLR_MAX_STACKS"); !ok {
		t.Setenv("GOT_GLR_MAX_STACKS", "8")
	}
	gotreesitter.ResetParseEnvConfigCacheForTests()
	t.Cleanup(gotreesitter.ResetParseEnvConfigCacheForTests)
	strict := scalaRealWorldStrictMode()

	repoDir := checkoutRealWorldRepo(t, scalaRealWorldRepoURL, scalaRealWorldCommit)

	for _, tc := range scalaRealWorldCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			expectation, ok := scalaRealWorldExpectations[tc.name]
			if !ok {
				t.Fatalf("missing scala real-world expectation for %q", tc.name)
			}

			absPath := filepath.Join(repoDir, filepath.FromSlash(tc.path))
			src, err := os.ReadFile(absPath)
			if err != nil {
				t.Fatalf("read scala corpus file %q: %v", tc.path, err)
			}
			if len(src) == 0 {
				t.Fatalf("empty scala corpus file %q", tc.path)
			}
			normalized := normalizedSource("scala", string(src))
			parityCase := parityCase{name: "scala", source: string(normalized)}

			goTree, goLang, err := parseWithGo(parityCase, normalized, nil)
			if err != nil {
				t.Fatalf("[scala/scala-realworld/%s] gotreesitter parse error: %v", tc.name, err)
			}
			defer goTree.Release()

			cLang, err := ParityCLanguage("scala")
			if err != nil {
				if skipReason := parityReferenceSkipReason(err); skipReason != "" {
					t.Skipf("[scala/scala-realworld/%s] skip C reference parser: %s", tc.name, skipReason)
				}
				t.Fatalf("[scala/scala-realworld/%s] load C parser from languages.lock: %v", tc.name, err)
			}
			cParser := sitter.NewParser()
			defer cParser.Close()
			if err := cParser.SetLanguage(cLang); err != nil {
				if skipReason := parityReferenceSkipReason(err); skipReason != "" {
					t.Skipf("[scala/scala-realworld/%s] skip C parser SetLanguage: %s", tc.name, skipReason)
				}
				t.Fatalf("[scala/scala-realworld/%s] C parser SetLanguage error: %v", tc.name, err)
			}
			cTree := cParser.Parse(normalized, nil)
			if cTree == nil || cTree.RootNode() == nil {
				t.Fatalf("[scala/scala-realworld/%s] C reference parser returned nil tree", tc.name)
			}
			defer cTree.Close()

			goRoot := goTree.RootNode()
			cRoot := cTree.RootNode()
			goRuntime := goTree.ParseRuntime()
			if goRuntime.Truncated {
				t.Fatalf("[scala/scala-realworld/%s] gotreesitter parse truncated: %s", tc.name, goRuntime.Summary())
			}
			if goRuntime.StopReason != gotreesitter.ParseStopAccepted {
				t.Fatalf("[scala/scala-realworld/%s] unexpected stop reason: %s", tc.name, goRuntime.Summary())
			}
			if cRoot.HasError() {
				t.Fatalf("[scala/scala-realworld/%s] C reference tree has errors unexpectedly", tc.name)
			}

			var errs []string
			compareNodes(goRoot, goLang, cRoot, "root", &errs)

			if strict {
				if len(errs) > 0 {
					t.Fatalf("[scala/scala-realworld/%s] strict parity expected zero divergence; got=%d first=%s", tc.name, len(errs), errs[0])
				}
				if goRoot.HasError() {
					t.Fatalf("[scala/scala-realworld/%s] strict parity requires no error nodes; got runtime=%s", tc.name, goRuntime.Summary())
				}
				return
			}

			if len(errs) > expectation.maxNodeDivergences {
				t.Fatalf(
					"[scala/scala-realworld/%s] divergence regression: got=%d max=%d first=%s go=%s c_root=%q",
					tc.name, len(errs), expectation.maxNodeDivergences, errs[0], goRuntime.Summary(), cRoot.Kind(),
				)
			}
			if goRoot.HasError() && !expectation.allowGoHasError {
				t.Fatalf(
					"[scala/scala-realworld/%s] regression: gotreesitter tree has errors unexpectedly (go=%s)",
					tc.name, goRuntime.Summary(),
				)
			}
			if len(errs) > 0 {
				t.Logf(
					"[scala/scala-realworld/%s] non-parity gap retained (within ratchet): divergences=%d max=%d first=%s",
					tc.name, len(errs), expectation.maxNodeDivergences, errs[0],
				)
			}
		})
	}
}

func scalaRealWorldStrictMode() bool {
	raw := strings.TrimSpace(os.Getenv("GTS_PARITY_SCALA_REALWORLD_STRICT"))
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

func checkoutRealWorldRepo(t *testing.T, repoURL, commit string) string {
	t.Helper()

	repoDir := filepath.Join(t.TempDir(), "repo")
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	runRetry := func(attempts int, args ...string) {
		t.Helper()
		if attempts < 1 {
			attempts = 1
		}
		var lastErr error
		for i := 0; i < attempts; i++ {
			cmd := exec.Command("git", args...)
			out, err := cmd.CombinedOutput()
			if err == nil {
				if len(out) > 0 {
					_, _ = os.Stdout.Write(out)
				}
				return
			}
			lastErr = fmt.Errorf("git %s: %v\n%s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			if !retryableGitNetworkError(string(out)) || i == attempts-1 {
				break
			}
			time.Sleep(time.Duration(i+1) * time.Second)
		}
		t.Fatal(lastErr)
	}

	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", repoDir, err)
	}
	run("-C", repoDir, "init")
	run("-C", repoDir, "remote", "add", "origin", repoURL)
	runRetry(5, "-C", repoDir, "fetch", "--depth=1", "origin", commit)
	run("-C", repoDir, "checkout", "--detach", "FETCH_HEAD")

	head := gitOutput(t, repoDir, "rev-parse", "HEAD")
	if !strings.HasPrefix(head, commit[:12]) {
		t.Fatalf("repo HEAD mismatch: got=%s want_prefix=%s", head, commit[:12])
	}
	return repoDir
}

func retryableGitNetworkError(msg string) bool {
	msg = strings.ToLower(msg)
	return strings.Contains(msg, "could not resolve host") ||
		strings.Contains(msg, "temporary failure in name resolution") ||
		strings.Contains(msg, "name or service not known") ||
		strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "operation timed out") ||
		strings.Contains(msg, "connection reset by peer")
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s output: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(string(out))
}

func TestParityScalaRealWorldCorpusMetadata(t *testing.T) {
	// Guard against accidental drift in the pinned corpus source.
	if !strings.HasPrefix(scalaRealWorldCommit, "9ca90550") {
		t.Fatalf("unexpected scala real-world commit pin: %s", scalaRealWorldCommit)
	}
	if len(scalaRealWorldCases) < 4 {
		t.Fatalf("insufficient scala real-world cases: %d", len(scalaRealWorldCases))
	}
	if len(scalaRealWorldExpectations) != len(scalaRealWorldCases) {
		t.Fatalf("expectation/case mismatch: expectations=%d cases=%d", len(scalaRealWorldExpectations), len(scalaRealWorldCases))
	}
	for _, c := range scalaRealWorldCases {
		if strings.TrimSpace(c.name) == "" || strings.TrimSpace(c.path) == "" {
			t.Fatalf("invalid scala real-world case: %+v", c)
		}
		if strings.Contains(c.path, "..") {
			t.Fatalf("invalid path traversal in scala case %q: %s", c.name, c.path)
		}
		if _, ok := scalaRealWorldExpectations[c.name]; !ok {
			t.Fatalf("missing expectation for scala case %q", c.name)
		}
	}
	t.Logf("scala real-world corpus: repo=%s commit=%s files=%d",
		scalaRealWorldRepoURL, scalaRealWorldCommit, len(scalaRealWorldCases))
}

func Example_scalaRealWorldCorpus() {
	fmt.Println("scala real-world structural parity corpus is pinned and reproducible")
	// Output: scala real-world structural parity corpus is pinned and reproducible
}
