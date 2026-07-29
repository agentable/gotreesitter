//go:build cgo && treesitter_c_parity

package cgoharness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// TestMeasureDtierVsC times the production (or forest) parser against the C
// oracle over a lock-filtered corpus dir and reports full_ratio + parity +
// truncation, so an unmeasured ("D") grammar can be classified into a tier.
//
//	REPRO_LANG   grammar name (must exist in grammars.AllLanguages)
//	REPRO_DIR    corpus root (per-lang subdir REPRO_DIR/REPRO_LANG)
//	REPRO_FILE   optional exact file path to measure instead of REPRO_DIR walk
//	REPRO_EXTS   comma list of extensions to keep (lock-filter; e.g. .agda)
//	REPRO_N      max files (default 40)
//	REPRO_ROUNDS timing parse rounds for Go and C min duration (default 3)
//	REPRO_FOREST =1 measure the forest path (recovery on) instead of production
//	REPRO_SIGNATURES=1 emit one compact DIVERGE-SIG line per divergent file
//	REPRO_PROGRESS=1 emit line-oriented per-file/phase progress telemetry
//
// Per-file panic recovery + the caller-set GOT_PARSE_MEMORY_BUDGET_MB keep a
// pathological blowup file from crashing the run (it yields a truncated tree).
func TestMeasureDtierVsC(t *testing.T) {
	if os.Getenv("REPRO_DIR") == "" {
		t.Skip("set REPRO_DIR")
	}
	name := os.Getenv("REPRO_LANG")
	root := os.Getenv("REPRO_DIR")
	reproFile := os.Getenv("REPRO_FILE")
	dir := filepath.Join(root, name)
	exts := strings.Split(os.Getenv("REPRO_EXTS"), ",")
	forest := os.Getenv("REPRO_FOREST") == "1"
	signatures := os.Getenv("REPRO_SIGNATURES") == "1"
	progress := os.Getenv("REPRO_PROGRESS") == "1"
	testStart := time.Now()
	progressf := func(format string, args ...any) {
		if !progress {
			return
		}
		fmt.Printf("MEASURE-PROGRESS "+format+"\n", args...)
	}

	var goLang *gts.Language
	for _, e := range grammars.AllLanguages() {
		if e.Name == name {
			goLang = e.Language()
			break
		}
	}
	if goLang == nil {
		t.Fatalf("%s: not in grammars.AllLanguages", name)
	}
	cLang, err := ParityCLanguage(name)
	if err != nil {
		t.Skipf("%s: no C reference: %v", name, err)
	}
	if forest {
		gts.SetGLRForestRecover(true)
		defer gts.SetGLRForestRecover(false)
	}

	var files []string
	if reproFile != "" {
		files = append(files, reproFile)
	} else {
		_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			if strings.Contains(p, "/.git/") {
				return nil
			}
			base := strings.ToLower(filepath.Base(p))
			for _, e := range exts {
				e = strings.ToLower(strings.TrimSpace(e))
				if e == "" {
					continue
				}
				if (strings.HasPrefix(e, ".") && strings.HasSuffix(base, e)) || base == e || strings.HasSuffix(base, "."+e) {
					if info.Size() >= 32 && info.Size() <= 200_000 {
						files = append(files, p)
					}
					break
				}
			}
			return nil
		})
		sort.Strings(files)
	}
	n := 40
	if v := os.Getenv("REPRO_N"); v != "" {
		fmt.Sscanf(v, "%d", &n)
	}
	rounds := 3
	if v := os.Getenv("REPRO_ROUNDS"); v != "" {
		var parsed int
		if _, err := fmt.Sscanf(v, "%d", &parsed); err == nil && parsed > 0 {
			rounds = parsed
		}
	}
	if reproFile == "" && n < len(files) {
		files = files[:n]
	}
	progressf("lang=%s total=%d phase=selected_files dir=%q n=%d rounds=%d elapsed_ms=%d", name, len(files), dir, n, rounds, time.Since(testStart).Milliseconds())
	if progress {
		total := len(files)
		for i, f := range files {
			bytes := int64(-1)
			if info, err := os.Stat(f); err == nil {
				bytes = info.Size()
			}
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=selected_file elapsed_ms=%d",
				name, i+1, total, filepath.Base(f), f, bytes, time.Since(testStart).Milliseconds())
		}
	}

	// minParse runs the parse for the configured rounds and returns the min wall time; recovers
	// panics (returns panicked=true) so one bad file cannot kill the run.
	// notAccepted == the parser stopped early (memory/no-stacks/node-limit) —
	// the REAL truncation signal (endByte<len is a false positive: trailing
	// comments/extras legitimately aren't covered by the root, same as C).
	minParse := func(src []byte, fileIndex, fileTotal int, path string, fileStart time.Time) (dur time.Duration, endByte uint32, hasErr, notAccepted bool, stopReason gts.ParseStopReason, runtimeSummary string, panicked bool) {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
				progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_parse_panic elapsed_ms=%d",
					name, fileIndex, fileTotal, filepath.Base(path), path, len(src), time.Since(fileStart).Milliseconds())
			}
		}()
		best := time.Duration(1<<62 - 1)
		for i := 0; i < rounds; i++ {
			p := gts.NewParser(goLang)
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_parse_start round=%d elapsed_ms=%d",
				name, fileIndex, fileTotal, filepath.Base(path), path, len(src), i+1, time.Since(fileStart).Milliseconds())
			t0 := time.Now()
			var tr *gts.Tree
			if forest {
				ft, _ := p.ParseForestExperimental(src)
				tr = ft
			} else {
				tr, _ = p.Parse(src)
			}
			d := time.Since(t0)
			if d < best {
				best = d
			}
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_parse_end round=%d elapsed_ms=%d duration_ms=%d",
				name, fileIndex, fileTotal, filepath.Base(path), path, len(src), i+1, time.Since(fileStart).Milliseconds(), d.Milliseconds())
			if i == rounds-1 && tr != nil && tr.RootNode() != nil {
				endByte = tr.RootNode().EndByte()
				hasErr = tr.RootNode().HasError()
				stopReason = tr.ParseStopReason()
				notAccepted = stopReason != gts.ParseStopAccepted
				runtimeSummary = tr.ParseRuntime().Summary()
			}
			if tr != nil {
				tr.Release()
			}
		}
		return best, endByte, hasErr, notAccepted, stopReason, runtimeSummary, false
	}

	var totGo, totC time.Duration
	var ratios []float64
	dispatched, matchC, divergeC, trunc, panics, errTree, oracleBetter := 0, 0, 0, 0, 0, 0, 0
	totalFiles := len(files)
	for fileIndex, f := range files {
		fileStart := time.Now()
		readStart := time.Now()
		progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=read_start elapsed_ms=%d",
			name, fileIndex+1, totalFiles, filepath.Base(f), f, -1, 0)
		src, rerr := os.ReadFile(f)
		if rerr != nil || len(src) == 0 {
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=read_skip elapsed_ms=%d error=%q",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds(), fmt.Sprint(rerr))
			continue
		}
		progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=read_end elapsed_ms=%d duration_ms=%d",
			name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds(), time.Since(readStart).Milliseconds())
		goDur, endByte, hasErr, notAccepted, stopReason, runtimeSummary, panicked := minParse(src, fileIndex+1, totalFiles, f, fileStart)
		if panicked {
			panics++
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=file_complete result=go_panic elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
			continue
		}
		_ = endByte
		if notAccepted {
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_parse_status result=not_accepted stopReason=%s runtime=%q elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), stopReason, runtimeSummary, time.Since(fileStart).Milliseconds())
		}
		// C oracle timing (min of configured rounds)
		cBest := time.Duration(1<<62 - 1)
		var cTree *sitter.Tree
		var cParser *sitter.Parser
		for i := 0; i < rounds; i++ {
			cParser = sitter.NewParser()
			_ = cParser.SetLanguage(cLang)
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=c_parse_start round=%d elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), i+1, time.Since(fileStart).Milliseconds())
			t0 := time.Now()
			ct := cParser.Parse(src, nil)
			d := time.Since(t0)
			if d < cBest {
				cBest = d
			}
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=c_parse_end round=%d elapsed_ms=%d duration_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), i+1, time.Since(fileStart).Milliseconds(), d.Milliseconds())
			if i == rounds-1 {
				cTree = ct
			} else if ct != nil {
				ct.Close()
			}
			if i < rounds-1 {
				cParser.Close()
			}
		}
		if cTree == nil || cTree.RootNode() == nil {
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=comparison_result result=c_no_tree elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
			if cParser != nil {
				cParser.Close()
			}
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=file_complete result=c_no_tree elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
			continue
		}
		dispatched++
		if notAccepted {
			trunc++
		}
		if hasErr {
			errTree++
		}
		// re-parse once for the comparison tree (timing runs released theirs)
		gp := gts.NewParser(goLang)
		var gtree *gts.Tree
		progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_compare_reparse_start elapsed_ms=%d",
			name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
		compareStart := time.Now()
		func() {
			defer func() { _ = recover() }()
			if forest {
				gtree, _ = gp.ParseForestExperimental(src)
			} else {
				gtree, _ = gp.Parse(src)
			}
		}()
		progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=go_compare_reparse_end elapsed_ms=%d duration_ms=%d",
			name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds(), time.Since(compareStart).Milliseconds())
		if gtree != nil && gtree.RootNode() != nil {
			compareRuntime := gtree.ParseRuntime().Summary()
			var errs []string
			compareNodes(gtree.RootNode(), goLang, cTree.RootNode(), "root", &errs)
			if len(errs) == 0 {
				matchC++
				progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=comparison_result result=match elapsed_ms=%d",
					name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
			} else {
				divergeC++
				goErrors, goMissing := fddCountGoIntegrity(gtree.RootNode(), goLang)
				cErrors, cMissing := fddCountCIntegrity(cTree.RootNode())
				oracleRelation := fddOracleRelation(goErrors, goMissing, cErrors, cMissing)
				if oracleRelation == "go_clean_c_error" {
					oracleBetter++
				}
				progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=comparison_result result=diverge errors=%d oracle=%q runtime=%q elapsed_ms=%d",
					name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), len(errs), oracleRelation, compareRuntime, time.Since(fileStart).Milliseconds())
				if os.Getenv("REPRO_DUMP_DIVERGENCE") == "1" && divergeC <= 6 {
					fmt.Printf("DIVERGE %s %s: %s\n", name, filepath.Base(f),
						strings.Join(errs[:min(2, len(errs))], " || "))
				}
				if sig := fddBuildSignature(f, gtree.RootNode(), goLang, cTree.RootNode(), src, string(gtree.ParseStopReason()), compareRuntime); sig != nil {
					fddPrintProgress(name, sig, fileIndex+1, totalFiles, len(src), time.Since(fileStart).Milliseconds())
					if signatures {
						fddPrintSignature(name, sig)
					}
				}
			}
			gtree.Release()
		} else {
			progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=comparison_result result=go_no_tree elapsed_ms=%d",
				name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds())
		}
		if cBest > 0 {
			ratios = append(ratios, float64(goDur)/float64(cBest))
		}
		totGo += goDur
		totC += cBest
		cTree.Close()
		cParser.Close()
		progressf("lang=%s file=%d/%d base=%q path=%q bytes=%d phase=file_complete elapsed_ms=%d go_ms=%d c_ms=%d",
			name, fileIndex+1, totalFiles, filepath.Base(f), f, len(src), time.Since(fileStart).Milliseconds(), goDur.Milliseconds(), cBest.Milliseconds())
	}

	median := 0.0
	if len(ratios) > 0 {
		sort.Float64s(ratios)
		median = ratios[len(ratios)/2]
	}
	agg := 0.0
	if totC > 0 {
		agg = float64(totGo) / float64(totC)
	}
	mode := "prod"
	if forest {
		mode = "forest"
	}
	parityPct := 0.0
	if dispatched > 0 {
		parityPct = 100 * float64(matchC) / float64(dispatched)
	}
	fmt.Printf("MEASURE-DTIER %s mode=%s files=%d medianRatio=%.2fx aggRatio=%.2fx parityMatch=%d/%d(%.0f%%) diverge=%d oracleBetter=%d trunc=%d errTree=%d panics=%d goNS=%d cNS=%d\n",
		name, mode, dispatched, median, agg, matchC, dispatched, parityPct, divergeC, oracleBetter, trunc, errTree, panics, totGo.Nanoseconds(), totC.Nanoseconds())
}
