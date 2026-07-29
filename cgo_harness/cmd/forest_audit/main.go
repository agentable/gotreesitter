package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	cgoharness "github.com/agentable/gotreesitter/cgo_harness"
	"github.com/agentable/gotreesitter/grammars"
)

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: forest_audit <manifest|reduce|plan-confirmations|publish-result|publish-confirmation|index-confirmations> [options]")
	}
	var err error
	switch os.Args[1] {
	case "manifest":
		err = runManifest(os.Args[2:])
	case "reduce":
		err = runReduce(os.Args[2:])
	case "plan-confirmations":
		err = runPlanConfirmations(os.Args[2:])
	case "publish-confirmation":
		err = runPublishConfirmation(os.Args[2:])
	case "publish-result":
		err = runPublishResult(os.Args[2:])
	case "index-confirmations":
		err = runIndexConfirmations(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func runManifest(args []string) error {
	flags := flag.NewFlagSet("manifest", flag.ContinueOnError)
	var revision, lockPath, corpusRoot, languagesRaw, order, excludesRaw, out string
	var maxFiles int
	var minBytes, maxBytes int64
	flags.StringVar(&revision, "gotreesitter-revision", "", "exact clean gotreesitter revision being audited")
	flags.StringVar(&lockPath, "corpus-lock", "", "authenticated corpus_sources.lock path")
	flags.StringVar(&corpusRoot, "corpus-root", "", "root containing one locked checkout per language")
	flags.StringVar(&languagesRaw, "langs", "all", "comma-separated languages or all")
	flags.StringVar(&order, "order", "largest", "selection order: largest, smallest, or path")
	flags.IntVar(&maxFiles, "max-files", 8, "maximum selected files per language; zero means all")
	flags.Int64Var(&minBytes, "min-file-bytes", 0, "minimum selected file size")
	flags.Int64Var(&maxBytes, "max-file-bytes", 4<<20, "maximum selected file size; zero means unlimited")
	flags.StringVar(&excludesRaw, "exclude-paths", "", "comma-separated exact or glob paths")
	flags.StringVar(&out, "out", "", "output manifest JSON path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if out == "" {
		return fmt.Errorf("--out is required")
	}
	languages := splitList(languagesRaw)
	if len(languages) == 1 && strings.EqualFold(languages[0], "all") {
		languages = nil
	}
	registryExtensions := map[string][]string{}
	for _, entry := range grammars.AllLanguages() {
		registryExtensions[entry.Name] = append([]string(nil), entry.Extensions...)
	}
	manifest, err := cgoharness.MaterializeForestCorpusManifest(cgoharness.ForestCorpusMaterializeOptions{
		GotreesitterRevision: revision,
		CorpusLockPath:       lockPath,
		CorpusRoot:           corpusRoot,
		Languages:            languages,
		RegistryExtensions:   registryExtensions,
		Selection: cgoharness.ForestCorpusSelection{
			Order: order, MaxFiles: maxFiles, MinFileBytes: minBytes,
			MaxFileBytes: maxBytes, ExcludePaths: splitList(excludesRaw),
		},
	})
	if err != nil {
		return err
	}
	return cgoharness.WriteForestCorpusManifest(out, manifest)
}

func runReduce(args []string) error {
	flags := flag.NewFlagSet("reduce", flag.ContinueOnError)
	var manifestPath, resultsRoot, confirmationIndex, out string
	flags.StringVar(&manifestPath, "manifest", "", "authenticated input manifest")
	flags.StringVar(&resultsRoot, "results-root", "", "root containing per-language result JSON")
	flags.StringVar(&confirmationIndex, "confirmation-index", "", "optional content-addressed confirmation index JSON")
	flags.StringVar(&out, "out", "", "output reduced report JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if manifestPath == "" || resultsRoot == "" || out == "" {
		return fmt.Errorf("--manifest, --results-root, and --out are required")
	}
	report, err := cgoharness.ReduceForestAuditResultsWithConfirmations(manifestPath, resultsRoot, confirmationIndex)
	if err != nil {
		return err
	}
	return cgoharness.WriteForestAuditReport(out, report)
}

func runPublishConfirmation(args []string) error {
	flags := flag.NewFlagSet("publish-confirmation", flag.ContinueOnError)
	var resultsRoot, runConfigPath, trialPath string
	flags.StringVar(&resultsRoot, "results-root", "", "forest audit bundle root")
	flags.StringVar(&runConfigPath, "run-config", "", "staged run-config JSON")
	flags.StringVar(&trialPath, "trial", "", "successful staged confirmation trial JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if resultsRoot == "" || runConfigPath == "" || trialPath == "" {
		return fmt.Errorf("--results-root, --run-config, and --trial are required")
	}
	stored, err := cgoharness.StoreForestAuditConfirmation(resultsRoot, runConfigPath, trialPath)
	if err != nil {
		return err
	}
	fmt.Printf("trial_sha256=%s\ncohort_sha256=%s\nconfirmation_index_sha256=%s\nconfirmation_index=%s\n",
		stored.TrialSHA256, stored.CohortSHA256, stored.IndexSHA256, stored.IndexPath)
	return nil
}

func runPublishResult(args []string) error {
	flags := flag.NewFlagSet("publish-result", flag.ContinueOnError)
	var resultsRoot, stagedPath string
	flags.StringVar(&resultsRoot, "results-root", "", "forest audit bundle root")
	flags.StringVar(&stagedPath, "result", "", "successful staged forest audit result JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if resultsRoot == "" || stagedPath == "" {
		return fmt.Errorf("--results-root and --result are required")
	}
	path, err := cgoharness.PublishForestAuditResult(resultsRoot, stagedPath)
	if err != nil {
		return err
	}
	fmt.Printf("result=%s\n", path)
	return nil
}

func runIndexConfirmations(args []string) error {
	flags := flag.NewFlagSet("index-confirmations", flag.ContinueOnError)
	var reportPath, resultsRoot, cohortsRaw string
	flags.StringVar(&reportPath, "report", "", "forest audit report providing the authenticated board identity")
	flags.StringVar(&resultsRoot, "results-root", "", "forest audit bundle root")
	flags.StringVar(&cohortsRaw, "cohorts", "", "comma-separated language=cohort_sha256 selections")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if reportPath == "" || resultsRoot == "" || cohortsRaw == "" {
		return fmt.Errorf("--report, --results-root, and --cohorts are required")
	}
	report, err := cgoharness.ReadForestAuditReport(reportPath)
	if err != nil {
		return err
	}
	index := cgoharness.ForestAuditConfirmationIndex{
		Schema: cgoharness.ForestAuditConfirmationIndexSchema, GotreesitterRevision: report.GotreesitterRevision,
		CorpusManifestSHA256: report.CorpusManifestSHA256, CorpusLockSHA256: report.CorpusLockSHA256,
	}
	for _, raw := range strings.Split(cohortsRaw, ",") {
		parts := strings.Split(strings.TrimSpace(raw), "=")
		if len(parts) != 2 {
			return fmt.Errorf("invalid cohort selection %q", raw)
		}
		index.Cohorts = append(index.Cohorts, cgoharness.ForestAuditConfirmationIndexEntry{Language: parts[0], CohortSHA256: parts[1]})
	}
	digest, path, err := cgoharness.PublishForestAuditConfirmationIndex(resultsRoot, index)
	if err != nil {
		return err
	}
	fmt.Printf("confirmation_index_sha256=%s\nconfirmation_index=%s\n", digest, path)
	return nil
}

func runPlanConfirmations(args []string) error {
	flags := flag.NewFlagSet("plan-confirmations", flag.ContinueOnError)
	var reportPath, out string
	flags.StringVar(&reportPath, "report", "", "forest audit report-v4 JSON")
	flags.StringVar(&out, "out", "", "output win-only confirmation plan JSON")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if reportPath == "" || out == "" {
		return fmt.Errorf("--report and --out are required")
	}
	report, err := cgoharness.ReadForestAuditReport(reportPath)
	if err != nil {
		return err
	}
	return cgoharness.WriteForestAuditConfirmationPlan(out, cgoharness.BuildForestAuditConfirmationPlan(report))
}

func splitList(raw string) []string {
	seen := map[string]bool{}
	var out []string
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item != "" && !seen[item] {
			seen[item] = true
			out = append(out, item)
		}
	}
	sort.Strings(out)
	return out
}

func fatalf(format string, args ...any) {
	program := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "%s: %s\n", program, fmt.Sprintf(format, args...))
	os.Exit(2)
}
