//go:build cgo && treesitter_c_parity && treesitter_c_perfscan && gts_workcount

package cgoharness

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"
	"time"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	"github.com/agentable/gotreesitter/internal/benchfixtures"
)

const retryProfileCertSchema = "gts-retry-profile-cert/v2"

type retryProfileCertAttempt struct {
	LogicalRung        string                       `json:"logical_rung"`
	OperationCause     string                       `json:"operation_cause"`
	StopReason         gotreesitter.ParseStopReason `json:"stop_reason"`
	RootHasError       bool                         `json:"root_has_error"`
	RootEndByte        uint32                       `json:"root_end_byte"`
	ResolvedMaxStacks  int                          `json:"resolved_max_stacks"`
	ResolvedRetryPass  bool                         `json:"resolved_retry_pass"`
	ResolvedMergeLimit int                          `json:"resolved_max_merge_per_key"`
}

type retryProfileCertParse struct {
	WallNanos  int64                        `json:"wall_nanos"`
	TotalAlloc uint64                       `json:"total_alloc_bytes"`
	Attempts   []retryProfileCertAttempt    `json:"attempts"`
	StopReason gotreesitter.ParseStopReason `json:"stop_reason"`
	RootStart  uint32                       `json:"root_start_byte"`
	RootEnd    uint32                       `json:"root_end_byte"`
	HasError   bool                         `json:"root_has_error"`
	DeepSHA256 string                       `json:"deep_tree_sha256"`
}

type retryProfileCertFile struct {
	Path                  string                `json:"path"`
	Bytes                 int                   `json:"bytes"`
	SourceSHA256          string                `json:"source_sha256"`
	Class                 string                `json:"class"`
	OracleDeepSHA256      string                `json:"oracle_deep_tree_sha256"`
	BaselineOracleParity  bool                  `json:"baseline_oracle_parity"`
	CandidateOracleParity bool                  `json:"candidate_oracle_parity"`
	OracleStatus          string                `json:"oracle_status"`
	OracleDetail          string                `json:"oracle_detail,omitempty"`
	Baseline              retryProfileCertParse `json:"baseline"`
	Candidate             retryProfileCertParse `json:"candidate"`
}

type retryProfileCertTotals struct {
	Files               int    `json:"files"`
	Bytes               int64  `json:"bytes"`
	BaselineWallNanos   int64  `json:"baseline_wall_nanos"`
	CandidateWallNanos  int64  `json:"candidate_wall_nanos"`
	BaselineTotalAlloc  uint64 `json:"baseline_total_alloc_bytes"`
	CandidateTotalAlloc uint64 `json:"candidate_total_alloc_bytes"`
	BaselineAttempts    int    `json:"baseline_attempts"`
	CandidateAttempts   int    `json:"candidate_attempts"`
	OracleMatches       int    `json:"oracle_matches"`
	OracleMismatches    int    `json:"oracle_mismatches"`
	OracleCrashes       int    `json:"oracle_crashes"`
	OracleUnavailable   int    `json:"oracle_unavailable"`
}

type retryProfileCertFailure struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

type retryProfileCertManifest struct {
	Schema               string                 `json:"schema"`
	Status               string                 `json:"status"`
	ResumeKey            string                 `json:"resume_key"`
	GeneratedAt          string                 `json:"generated_at"`
	Language             string                 `json:"language"`
	BlobSHA256           string                 `json:"blob_sha256"`
	CandidateRevision    string                 `json:"candidate_revision"`
	CorpusRoot           string                 `json:"corpus_root"`
	CorpusLock           string                 `json:"corpus_lock"`
	CorpusLockSHA256     string                 `json:"corpus_lock_sha256"`
	CorpusManifestSHA256 string                 `json:"corpus_manifest_sha256"`
	Oracle               perfScanOracleIdentity `json:"oracle"`
	OracleIdentitySHA256 string                 `json:"oracle_identity_sha256"`
	ParserConfig         []string               `json:"parser_config"`
	SelectionConfig      []string               `json:"selection_config"`
	CandidateProfile     struct {
		SkipExternalScannerRepeat bool `json:"skip_external_scanner_repeat"`
	} `json:"candidate_profile"`
	Totals  retryProfileCertTotals   `json:"totals"`
	Clean   retryProfileCertTotals   `json:"clean"`
	Error   retryProfileCertTotals   `json:"error"`
	Files   []retryProfileCertFile   `json:"files"`
	Failure *retryProfileCertFailure `json:"failure,omitempty"`
}

type retryProfileCertJournalRecord struct {
	Schema    string               `json:"schema"`
	ResumeKey string               `json:"resume_key"`
	File      retryProfileCertFile `json:"file"`
}

type retryProfileCertJournal struct {
	file  *os.File
	prior map[string]retryProfileCertFile
	key   string
}

func TestRetryProfileCorpusCertification(t *testing.T) {
	if strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT")) != "1" {
		t.Skip("set GTS_RETRY_PROFILE_CERT=1 to certify a retry profile")
	}
	name := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_LANG"))
	if name == "" || strings.Contains(name, ",") {
		t.Fatal("GTS_RETRY_PROFILE_CERT_LANG must name exactly one language")
	}
	blob := grammars.BlobByName(name)
	if len(blob) == 0 {
		t.Fatalf("BlobByName(%s) returned no data", name)
	}
	candidate, err := grammars.LoadLanguage(name, blob)
	if err != nil {
		t.Fatalf("load candidate language: %v", err)
	}
	baseline, err := grammars.LoadLanguage(name, blob)
	if err != nil {
		t.Fatalf("load baseline language: %v", err)
	}
	baseline.ExternalScannerFullParseRetryPolicy = gotreesitter.ExternalScannerFullParseRetryDefault
	if candidate.ExternalScannerFullParseRetryPolicy != gotreesitter.ExternalScannerFullParseRetrySkipRepeat {
		t.Fatalf("candidate %s does not suppress the external-scanner repeat", name)
	}
	staticOracle, err := buildStaticCPerfOracle(name)
	if err != nil {
		t.Fatalf("build locked static C oracle: %v", err)
	}
	defer staticOracle.Close()

	root := realCorpusBenchmarkRootForTest(t)
	langRoot := realCorpusBenchmarkLanguageRoot(t, root, name)
	files := loadRealCorpusBenchmarkFiles(t, langRoot, realCorpusBenchmarkFileFiltersFor(t, name, root))
	lockPath := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_LOCK"))
	if lockPath == "" {
		t.Fatal("set GTS_REAL_CORPUS_BENCH_LOCK to the authenticated corpus lock")
	}
	lockData, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read corpus lock: %v", err)
	}
	blobSum := sha256.Sum256(blob)
	lockSum := sha256.Sum256(lockData)
	candidateRevision, err := retryProfileCertCandidateRevision()
	if err != nil {
		t.Fatalf("authenticate candidate revision: %v", err)
	}
	oracleIdentityData, err := json.Marshal(staticOracle.identity)
	if err != nil {
		t.Fatalf("encode locked static C oracle identity: %v", err)
	}
	oracleIdentitySum := sha256.Sum256(oracleIdentityData)
	manifest := retryProfileCertManifest{
		Schema:               retryProfileCertSchema,
		GeneratedAt:          time.Now().UTC().Format(time.RFC3339),
		Language:             name,
		BlobSHA256:           hex.EncodeToString(blobSum[:]),
		CandidateRevision:    candidateRevision,
		CorpusRoot:           langRoot,
		CorpusLock:           lockPath,
		CorpusLockSHA256:     hex.EncodeToString(lockSum[:]),
		Oracle:               staticOracle.identity,
		OracleIdentitySHA256: hex.EncodeToString(oracleIdentitySum[:]),
		ParserConfig:         retryProfileCertParserConfig(),
		SelectionConfig:      retryProfileCertSelectionConfig(),
		Files:                make([]retryProfileCertFile, 0, len(files)),
	}
	manifest.ResumeKey = retryProfileCertResumeKey(manifest)
	manifest.CandidateProfile.SkipExternalScannerRepeat = true
	journal, err := openRetryProfileCertJournal(manifest)
	if err != nil {
		t.Fatalf("open retry-profile journal: %v", err)
	}
	if journal != nil {
		defer journal.Close()
	}
	defer func() {
		if manifest.Status == "" {
			manifest.Status = "failed"
		}
		if err := retryProfileCertWriteManifest(manifest); err != nil {
			t.Errorf("write retry-profile receipt: %v", err)
		}
	}()
	corpusHash := sha256.New()

	for _, file := range files {
		rel, _ := filepath.Rel(langRoot, file.path)
		sourceSum := sha256.Sum256(file.source)
		_, _ = fmt.Fprintf(corpusHash, "%s\x00%d\x00%x\n", filepath.ToSlash(rel), len(file.source), sourceSum)
		manifest.CorpusManifestSHA256 = hex.EncodeToString(corpusHash.Sum(nil))
		if journal != nil {
			if prior, ok, err := retryProfileCertResumeRow(journal, filepath.ToSlash(rel), file.source); err != nil {
				manifest.Failure = &retryProfileCertFailure{Path: filepath.ToSlash(rel), Reason: "invalid resumed row: " + err.Error()}
				t.Fatalf("resume %s: %v", rel, err)
			} else if ok {
				retryProfileCertAccumulate(&manifest, prior)
				continue
			}
		}

		baselineResult, baselineTree := retryProfileCertParseOne(t, baseline, file.source)
		candidateResult, candidateTree := retryProfileCertParseOne(t, candidate, file.source)
		oracleDigest, oracleStatus, oracleDetail := retryProfileCertStaticOracle(staticOracle, file.source)
		class := "clean"
		if candidateResult.HasError {
			class = "error"
		}
		row := retryProfileCertFile{
			Path:                  filepath.ToSlash(rel),
			Bytes:                 len(file.source),
			SourceSHA256:          hex.EncodeToString(sourceSum[:]),
			Class:                 class,
			OracleDeepSHA256:      oracleDigest,
			BaselineOracleParity:  oracleDigest != "" && baselineResult.DeepSHA256 == oracleDigest,
			CandidateOracleParity: oracleDigest != "" && candidateResult.DeepSHA256 == oracleDigest,
			OracleStatus:          oracleStatus,
			OracleDetail:          oracleDetail,
			Baseline:              baselineResult,
			Candidate:             candidateResult,
		}
		if row.OracleStatus == "completed" {
			if row.CandidateOracleParity {
				row.OracleStatus = "matched"
			} else {
				row.OracleStatus = "mismatch"
			}
		}
		if err := validateRetryProfileCertRow(row, filepath.ToSlash(rel), file.source); err != nil {
			baselineTree.Release()
			candidateTree.Release()
			manifest.Failure = &retryProfileCertFailure{Path: row.Path, Reason: err.Error()}
			t.Fatalf("%s: %v", rel, err)
		}
		if journal != nil {
			if err := journal.Append(row); err != nil {
				baselineTree.Release()
				candidateTree.Release()
				t.Fatalf("append retry-profile journal: %v", err)
			}
		}
		retryProfileCertAccumulate(&manifest, row)
		baselineTree.Release()
		candidateTree.Release()
	}
	if journal != nil && len(journal.prior) != 0 {
		paths := make([]string, 0, len(journal.prior))
		for path := range journal.prior {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		manifest.Failure = &retryProfileCertFailure{Reason: fmt.Sprintf("journal contains %d unselected rows", len(paths))}
		t.Fatalf("journal contains unselected rows: %s", strings.Join(paths, ", "))
	}
	if manifest.Totals.BaselineAttempts <= manifest.Totals.CandidateAttempts {
		manifest.Failure = &retryProfileCertFailure{Reason: "candidate did not eliminate retry attempts"}
		t.Fatalf("candidate did not eliminate retry attempts: baseline=%d candidate=%d", manifest.Totals.BaselineAttempts, manifest.Totals.CandidateAttempts)
	}
	if manifest.Totals.CandidateWallNanos*100 > manifest.Totals.BaselineWallNanos*98 {
		manifest.Failure = &retryProfileCertFailure{Reason: "candidate wall-time improvement is below 2%"}
		t.Fatalf("candidate wall-time improvement below 2%%: baseline=%s candidate=%s",
			time.Duration(manifest.Totals.BaselineWallNanos), time.Duration(manifest.Totals.CandidateWallNanos))
	}
	manifest.Status = "passed"
	t.Logf("retry profile certified: language=%s files=%d bytes=%d attempts=%d->%d wall=%s->%s oracle=%d_match/%d_mismatch/%d_crash/%d_unavailable manifest=%s",
		name, manifest.Totals.Files, manifest.Totals.Bytes,
		manifest.Totals.BaselineAttempts, manifest.Totals.CandidateAttempts,
		time.Duration(manifest.Totals.BaselineWallNanos), time.Duration(manifest.Totals.CandidateWallNanos),
		manifest.Totals.OracleMatches, manifest.Totals.OracleMismatches,
		manifest.Totals.OracleCrashes, manifest.Totals.OracleUnavailable,
		manifest.CorpusManifestSHA256)
}

func retryProfileCertEquivalent(baseline, candidate retryProfileCertParse) bool {
	return baseline.DeepSHA256 == candidate.DeepSHA256 &&
		baseline.StopReason == candidate.StopReason &&
		baseline.RootStart == candidate.RootStart &&
		baseline.RootEnd == candidate.RootEnd &&
		baseline.HasError == candidate.HasError
}

func validateRetryProfileCertRow(row retryProfileCertFile, wantPath string, source []byte) error {
	cleanPath := filepath.ToSlash(filepath.Clean(row.Path))
	if row.Path == "" || filepath.IsAbs(row.Path) || cleanPath != row.Path || row.Path == ".." || strings.HasPrefix(row.Path, "../") {
		return fmt.Errorf("invalid normalized path %q", row.Path)
	}
	if row.Path != wantPath {
		return fmt.Errorf("path=%q want=%q", row.Path, wantPath)
	}
	if row.Bytes != len(source) {
		return fmt.Errorf("bytes=%d want=%d", row.Bytes, len(source))
	}
	sourceSum := sha256.Sum256(source)
	if row.SourceSHA256 != hex.EncodeToString(sourceSum[:]) {
		return fmt.Errorf("source sha256=%q want=%x", row.SourceSHA256, sourceSum)
	}
	if err := validateRetryProfileCertParse("baseline", row.Baseline, len(source)); err != nil {
		return err
	}
	if err := validateRetryProfileCertParse("candidate", row.Candidate, len(source)); err != nil {
		return err
	}
	if !retryProfileCertEquivalent(row.Baseline, row.Candidate) {
		return fmt.Errorf("baseline and candidate parse results differ")
	}
	wantClass := "clean"
	if row.Candidate.HasError {
		wantClass = "error"
	}
	if row.Class != wantClass {
		return fmt.Errorf("class=%q want=%q", row.Class, wantClass)
	}
	baselineOracleParity := row.OracleDeepSHA256 != "" && row.Baseline.DeepSHA256 == row.OracleDeepSHA256
	candidateOracleParity := row.OracleDeepSHA256 != "" && row.Candidate.DeepSHA256 == row.OracleDeepSHA256
	if row.BaselineOracleParity != baselineOracleParity {
		return fmt.Errorf("baseline_oracle_parity=%t want=%t from authenticated digests", row.BaselineOracleParity, baselineOracleParity)
	}
	if row.CandidateOracleParity != candidateOracleParity {
		return fmt.Errorf("candidate_oracle_parity=%t want=%t from authenticated digests", row.CandidateOracleParity, candidateOracleParity)
	}
	if row.BaselineOracleParity != row.CandidateOracleParity {
		return fmt.Errorf("baseline/candidate oracle relation differs")
	}
	switch row.OracleStatus {
	case "matched":
		if !validRetryProfileCertSHA256(row.OracleDeepSHA256) || !row.BaselineOracleParity || !row.CandidateOracleParity || row.OracleDetail != "" {
			return fmt.Errorf("invalid matched oracle result")
		}
	case "mismatch":
		if !validRetryProfileCertSHA256(row.OracleDeepSHA256) || row.BaselineOracleParity || row.CandidateOracleParity || row.OracleDetail != "" {
			return fmt.Errorf("invalid mismatched oracle result")
		}
	case "oracle_crash",
		staticCStatusAdmissionError,
		staticCStatusParserError, staticCStatusParserTimeout,
		staticCStatusTransportError, staticCStatusTransportTimeout,
		staticCStatusDigestError, staticCStatusDigestTimeout,
		staticCStatusProtocolError, staticCStatusIncomplete:
		if row.OracleDeepSHA256 != "" || row.BaselineOracleParity || row.CandidateOracleParity || strings.TrimSpace(row.OracleDetail) == "" {
			return fmt.Errorf("invalid unavailable oracle result status=%q", row.OracleStatus)
		}
	default:
		return fmt.Errorf("invalid oracle status %q", row.OracleStatus)
	}
	return nil
}

func retryProfileCertResumeRow(journal *retryProfileCertJournal, path string, source []byte) (retryProfileCertFile, bool, error) {
	if journal == nil {
		return retryProfileCertFile{}, false, nil
	}
	row, ok := journal.prior[path]
	if !ok {
		return retryProfileCertFile{}, false, nil
	}
	if err := validateRetryProfileCertRow(row, path, source); err != nil {
		return retryProfileCertFile{}, false, err
	}
	delete(journal.prior, path)
	return row, true, nil
}

func validateRetryProfileCertParse(label string, parse retryProfileCertParse, sourceBytes int) error {
	if parse.WallNanos <= 0 {
		return fmt.Errorf("%s wall_nanos=%d", label, parse.WallNanos)
	}
	if !validRetryProfileCertSHA256(parse.DeepSHA256) {
		return fmt.Errorf("%s has invalid deep digest %q", label, parse.DeepSHA256)
	}
	if parse.StopReason != gotreesitter.ParseStopAccepted {
		return fmt.Errorf("%s stop_reason=%q want=%q", label, parse.StopReason, gotreesitter.ParseStopAccepted)
	}
	if parse.RootStart > parse.RootEnd || parse.RootEnd != uint32(sourceBytes) {
		return fmt.Errorf("%s root=%d..%d does not cover EOF=%d", label, parse.RootStart, parse.RootEnd, sourceBytes)
	}
	if len(parse.Attempts) == 0 {
		return fmt.Errorf("%s has no parse attempts", label)
	}
	for i, attempt := range parse.Attempts {
		if strings.TrimSpace(attempt.LogicalRung) == "" || strings.TrimSpace(attempt.OperationCause) == "" {
			return fmt.Errorf("%s attempt %d lacks rung/cause", label, i)
		}
		if !validRetryProfileCertStopReason(attempt.StopReason) {
			return fmt.Errorf("%s attempt %d has invalid stop reason %q", label, i, attempt.StopReason)
		}
		if attempt.RootEndByte > uint32(sourceBytes) || attempt.ResolvedMaxStacks <= 0 || attempt.ResolvedMergeLimit <= 0 {
			return fmt.Errorf("%s attempt %d has invalid bounds", label, i)
		}
	}
	return nil
}

func validRetryProfileCertSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validRetryProfileCertStopReason(reason gotreesitter.ParseStopReason) bool {
	switch reason {
	case gotreesitter.ParseStopNone,
		gotreesitter.ParseStopAccepted,
		gotreesitter.ParseStopNoStacksAlive,
		gotreesitter.ParseStopTokenSourceEOF,
		gotreesitter.ParseStopTimeout,
		gotreesitter.ParseStopCancelled,
		gotreesitter.ParseStopIterationLimit,
		gotreesitter.ParseStopStackDepthLimit,
		gotreesitter.ParseStopNodeLimit,
		gotreesitter.ParseStopMemoryBudget:
		return true
	default:
		return false
	}
}

func retryProfileCertAccumulate(manifest *retryProfileCertManifest, row retryProfileCertFile) {
	manifest.Files = append(manifest.Files, row)
	retryProfileCertAddTotals(&manifest.Totals, row)
	if row.Class == "error" {
		retryProfileCertAddTotals(&manifest.Error, row)
	} else {
		retryProfileCertAddTotals(&manifest.Clean, row)
	}
}

func retryProfileCertResumeKey(manifest retryProfileCertManifest) string {
	parts := []string{
		manifest.Schema,
		manifest.Language,
		manifest.BlobSHA256,
		manifest.CandidateRevision,
		manifest.CorpusRoot,
		manifest.CorpusLock,
		manifest.CorpusLockSHA256,
		manifest.OracleIdentitySHA256,
		"skip_external_scanner_repeat=true",
		"parser_config",
	}
	parts = append(parts, manifest.ParserConfig...)
	parts = append(parts, "selection_config")
	parts = append(parts, manifest.SelectionConfig...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(sum[:])
}

func openRetryProfileCertJournal(manifest retryProfileCertManifest) (*retryProfileCertJournal, error) {
	out := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_OUT"))
	if out == "" {
		return nil, nil
	}
	path := out + ".files.jsonl"
	journal := &retryProfileCertJournal{key: manifest.ResumeKey, prior: make(map[string]retryProfileCertFile)}
	if strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_RESUME")) == "1" {
		input, err := os.Open(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if input != nil {
			scanner := bufio.NewScanner(input)
			scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
			for scanner.Scan() {
				var record retryProfileCertJournalRecord
				if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
					_ = input.Close()
					return nil, fmt.Errorf("decode %s: %w", path, err)
				}
				if record.Schema != retryProfileCertSchema {
					_ = input.Close()
					return nil, fmt.Errorf("%s contains schema %q, want %q", path, record.Schema, retryProfileCertSchema)
				}
				if record.ResumeKey != manifest.ResumeKey {
					_ = input.Close()
					return nil, fmt.Errorf("%s belongs to resume key %s, want %s", path, record.ResumeKey, manifest.ResumeKey)
				}
				if _, exists := journal.prior[record.File.Path]; exists {
					_ = input.Close()
					return nil, fmt.Errorf("%s contains duplicate path %q", path, record.File.Path)
				}
				journal.prior[record.File.Path] = record.File
			}
			if err := scanner.Err(); err != nil {
				_ = input.Close()
				return nil, err
			}
			if err := input.Close(); err != nil {
				return nil, err
			}
		}
	} else if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	journal.file = file
	return journal, nil
}

func (journal *retryProfileCertJournal) Append(row retryProfileCertFile) error {
	data, err := json.Marshal(retryProfileCertJournalRecord{Schema: retryProfileCertSchema, ResumeKey: journal.key, File: row})
	if err != nil {
		return err
	}
	_, err = journal.file.Write(append(data, '\n'))
	return err
}

func (journal *retryProfileCertJournal) Close() error {
	if journal == nil || journal.file == nil {
		return nil
	}
	return journal.file.Close()
}

func retryProfileCertWriteManifest(manifest retryProfileCertManifest) error {
	out := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_OUT"))
	if out == "" {
		return nil
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	return os.WriteFile(out, append(data, '\n'), 0o644)
}

func retryProfileCertStaticOracle(oracle *staticCPerfOracle, source []byte) (digest, status, detail string) {
	digest, _, err := oracle.deepDigest(source, 10*time.Second)
	if err == nil {
		return digest, "completed", ""
	}
	status = staticCOracleErrorStatus(err)
	detail = err.Error()
	lower := strings.ToLower(detail)
	if strings.Contains(lower, "sigabrt") || strings.Contains(lower, "signal: aborted") || strings.Contains(lower, "assertion") {
		status = "oracle_crash"
	}
	return "", status, detail
}

func realCorpusBenchmarkRootForTest(t testing.TB) string {
	t.Helper()
	if root := strings.TrimSpace(os.Getenv("GTS_REAL_CORPUS_BENCH_ROOT")); root != "" {
		return root
	}
	t.Fatal("set GTS_REAL_CORPUS_BENCH_ROOT")
	return ""
}

func retryProfileCertParseOne(t testing.TB, lang *gotreesitter.Language, source []byte) (retryProfileCertParse, *gotreesitter.Tree) {
	t.Helper()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	gotreesitter.BeginDiagnosticWorkCount()
	start := time.Now()
	tree, err := gotreesitter.NewParser(lang).Parse(source)
	wall := time.Since(start)
	counts := gotreesitter.EndDiagnosticWorkCount()
	runtime.ReadMemStats(&after)
	if err != nil {
		if tree != nil {
			tree.Release()
		}
		t.Fatalf("parse: %v", err)
	}
	if tree == nil || tree.RootNode() == nil {
		t.Fatal("parse returned no tree")
	}
	inspection, err := benchfixtures.InspectGoTree(tree.RootNode(), lang)
	if err != nil {
		tree.Release()
		t.Fatalf("deep digest: %v", err)
	}
	rt := tree.ParseRuntime()
	result := retryProfileCertParse{
		WallNanos:  wall.Nanoseconds(),
		TotalAlloc: after.TotalAlloc - before.TotalAlloc,
		StopReason: rt.StopReason,
		RootStart:  tree.RootNode().StartByte(),
		RootEnd:    tree.RootNode().EndByte(),
		HasError:   tree.RootNode().HasError(),
		DeepSHA256: inspection.SHA256,
		Attempts:   make([]retryProfileCertAttempt, 0, len(counts.Attempts)),
	}
	for _, attempt := range counts.Attempts {
		result.Attempts = append(result.Attempts, retryProfileCertAttempt{
			LogicalRung:        attempt.LogicalRung,
			OperationCause:     attempt.OperationCause,
			StopReason:         attempt.StopReason,
			RootHasError:       attempt.RootHasError,
			RootEndByte:        attempt.RootEndByte,
			ResolvedMaxStacks:  attempt.ResolvedMaxStacks,
			ResolvedRetryPass:  attempt.ResolvedRetryPass,
			ResolvedMergeLimit: attempt.ResolvedMaxMergePerKey,
		})
	}
	return result, tree
}

func retryProfileCertAddTotals(totals *retryProfileCertTotals, row retryProfileCertFile) {
	totals.Files++
	totals.Bytes += int64(row.Bytes)
	totals.BaselineWallNanos += row.Baseline.WallNanos
	totals.CandidateWallNanos += row.Candidate.WallNanos
	totals.BaselineTotalAlloc += row.Baseline.TotalAlloc
	totals.CandidateTotalAlloc += row.Candidate.TotalAlloc
	totals.BaselineAttempts += len(row.Baseline.Attempts)
	totals.CandidateAttempts += len(row.Candidate.Attempts)
	switch row.OracleStatus {
	case "matched":
		totals.OracleMatches++
	case "mismatch":
		totals.OracleMismatches++
	case "oracle_crash":
		totals.OracleCrashes++
	default:
		totals.OracleUnavailable++
	}
}

func retryProfileCertCandidateRevision() (string, error) {
	explicit := strings.TrimSpace(os.Getenv("GTS_RETRY_PROFILE_CERT_CANDIDATE_REVISION"))
	buildRevision, buildModified := retryProfileCertBuildRevision()
	if explicit == "" {
		if buildRevision == "" {
			return "", fmt.Errorf("build revision unavailable; set GTS_RETRY_PROFILE_CERT_CANDIDATE_REVISION to the exact clean HEAD")
		}
		explicit = buildRevision
	}
	if !validRetryProfileCertRevision(explicit) {
		return "", fmt.Errorf("candidate revision %q is not a canonical 40- or 64-digit lowercase hex revision", explicit)
	}
	if buildModified {
		return "", fmt.Errorf("candidate build reports vcs.modified=true")
	}
	if buildRevision != "" && buildRevision != explicit {
		return "", fmt.Errorf("candidate revision %s differs from build revision %s", explicit, buildRevision)
	}
	head, err := retryProfileCertGitOutput("rev-parse", "--verify", "HEAD")
	if err != nil {
		return "", fmt.Errorf("authenticate git HEAD: %w", err)
	}
	if head != explicit {
		return "", fmt.Errorf("candidate revision %s differs from git HEAD %s", explicit, head)
	}
	resolved, err := retryProfileCertGitOutput("rev-parse", "--verify", explicit+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve candidate revision: %w", err)
	}
	if resolved != explicit {
		return "", fmt.Errorf("candidate revision %s resolves to %s", explicit, resolved)
	}
	status, err := retryProfileCertGitOutput("status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return "", fmt.Errorf("authenticate clean candidate worktree: %w", err)
	}
	if status != "" {
		return "", fmt.Errorf("candidate worktree is dirty")
	}
	return explicit, nil
}

func retryProfileCertBuildRevision() (string, bool) {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	var revision string
	var modified bool
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if validRetryProfileCertRevision(setting.Value) {
				revision = setting.Value
			}
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	return revision, modified
}

func validRetryProfileCertRevision(value string) bool {
	if (len(value) != 40 && len(value) != 64) || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func retryProfileCertGitOutput(args ...string) (string, error) {
	command := exec.Command("git", args...)
	data, err := command.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

func retryProfileCertParserConfig() []string {
	return retryProfileCertEnvSnapshot(func(key string) bool {
		return strings.HasPrefix(key, "GOT_") ||
			strings.HasPrefix(key, "GOTREESITTER_GRAMMAR_") ||
			key == "GOMAXPROCS" || key == "GOMEMLIMIT" || key == "GODEBUG"
	})
}

func retryProfileCertSelectionConfig() []string {
	return retryProfileCertEnvSnapshot(func(key string) bool {
		return strings.HasPrefix(key, "GTS_REAL_CORPUS_BENCH_") ||
			key == "GTS_RETRY_PROFILE_CERT_LANG" || key == "GTS_C_ORACLE_CACHE"
	})
}

func retryProfileCertEnvSnapshot(include func(string) bool) []string {
	config := make([]string, 0)
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && include(key) {
			config = append(config, entry)
		}
	}
	sort.Strings(config)
	return config
}

func TestRetryProfileCertResumeRejectsCounterexample(t *testing.T) {
	source := []byte("x")
	row := retryProfileCertValidTestRow(source)
	otherDigest := sha256.Sum256([]byte("counterexample"))
	row.Candidate.DeepSHA256 = hex.EncodeToString(otherDigest[:])

	out := filepath.Join(t.TempDir(), "receipt.json")
	t.Setenv("GTS_RETRY_PROFILE_CERT_OUT", out)
	t.Setenv("GTS_RETRY_PROFILE_CERT_RESUME", "1")
	manifest := retryProfileCertManifest{ResumeKey: strings.Repeat("a", sha256.Size*2)}
	record, err := json.Marshal(retryProfileCertJournalRecord{
		Schema:    retryProfileCertSchema,
		ResumeKey: manifest.ResumeKey,
		File:      row,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".files.jsonl", append(record, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := openRetryProfileCertJournal(manifest)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer journal.Close()
	if _, ok, err := retryProfileCertResumeRow(journal, row.Path, source); err == nil || ok {
		t.Fatalf("counterexample resumed: ok=%v err=%v", ok, err)
	}
	if _, exists := journal.prior[row.Path]; !exists {
		t.Fatal("invalid resumed row was consumed")
	}
}

func TestRetryProfileCertResumeRejectsForgedOracleRelation(t *testing.T) {
	source := []byte("x")
	row := retryProfileCertValidTestRow(source)
	otherDigest := sha256.Sum256([]byte("different oracle tree"))
	row.OracleDeepSHA256 = hex.EncodeToString(otherDigest[:])
	// These persisted claims are internally consistent with the status, but not
	// with either authenticated parse digest. Resume must derive the relation
	// again instead of trusting the journal's booleans.
	row.BaselineOracleParity = true
	row.CandidateOracleParity = true
	row.OracleStatus = "matched"

	out := filepath.Join(t.TempDir(), "receipt.json")
	t.Setenv("GTS_RETRY_PROFILE_CERT_OUT", out)
	t.Setenv("GTS_RETRY_PROFILE_CERT_RESUME", "1")
	manifest := retryProfileCertManifest{ResumeKey: strings.Repeat("c", sha256.Size*2)}
	record, err := json.Marshal(retryProfileCertJournalRecord{
		Schema:    retryProfileCertSchema,
		ResumeKey: manifest.ResumeKey,
		File:      row,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".files.jsonl", append(record, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	journal, err := openRetryProfileCertJournal(manifest)
	if err != nil {
		t.Fatalf("open journal: %v", err)
	}
	defer journal.Close()
	if _, ok, err := retryProfileCertResumeRow(journal, row.Path, source); err == nil || ok {
		t.Fatalf("forged oracle relation resumed: ok=%v err=%v", ok, err)
	}
	if _, exists := journal.prior[row.Path]; !exists {
		t.Fatal("invalid resumed row was consumed")
	}
}

func TestRetryProfileCertJournalRejectsMixedSchema(t *testing.T) {
	out := filepath.Join(t.TempDir(), "receipt.json")
	t.Setenv("GTS_RETRY_PROFILE_CERT_OUT", out)
	t.Setenv("GTS_RETRY_PROFILE_CERT_RESUME", "1")
	manifest := retryProfileCertManifest{ResumeKey: strings.Repeat("b", sha256.Size*2)}
	record, err := json.Marshal(retryProfileCertJournalRecord{
		Schema:    "gts-retry-profile-cert/v1",
		ResumeKey: manifest.ResumeKey,
		File:      retryProfileCertValidTestRow([]byte("x")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out+".files.jsonl", append(record, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if journal, err := openRetryProfileCertJournal(manifest); err == nil {
		if journal != nil {
			_ = journal.Close()
		}
		t.Fatal("mixed-schema journal was accepted")
	}
}

func TestRetryProfileCertAllowsMatchingRootAfterLeadingTrivia(t *testing.T) {
	source := []byte("\nx")
	row := retryProfileCertValidTestRow(source)
	row.Path = "x.m"
	row.Baseline.RootStart = 1
	row.Candidate.RootStart = 1
	if err := validateRetryProfileCertRow(row, row.Path, source); err != nil {
		t.Fatalf("validate matching nonzero root start: %v", err)
	}
}

func retryProfileCertValidTestRow(source []byte) retryProfileCertFile {
	sourceDigest := sha256.Sum256(source)
	treeDigest := sha256.Sum256([]byte("tree"))
	parse := retryProfileCertParse{
		WallNanos:  1,
		TotalAlloc: 1,
		Attempts: []retryProfileCertAttempt{{
			LogicalRung:        "initial",
			OperationCause:     "full_parse",
			StopReason:         gotreesitter.ParseStopAccepted,
			RootEndByte:        uint32(len(source)),
			ResolvedMaxStacks:  1,
			ResolvedMergeLimit: 1,
		}},
		StopReason: gotreesitter.ParseStopAccepted,
		RootEnd:    uint32(len(source)),
		DeepSHA256: hex.EncodeToString(treeDigest[:]),
	}
	return retryProfileCertFile{
		Path:                  "x.cr",
		Bytes:                 len(source),
		SourceSHA256:          hex.EncodeToString(sourceDigest[:]),
		Class:                 "clean",
		OracleDeepSHA256:      parse.DeepSHA256,
		BaselineOracleParity:  true,
		CandidateOracleParity: true,
		OracleStatus:          "matched",
		Baseline:              parse,
		Candidate:             parse,
	}
}
