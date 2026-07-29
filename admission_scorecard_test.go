//go:build gts_parsercorephase0

package gotreesitter_test

import (
	"fmt"
	"os"
	"sort"
	"testing"

	gts "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	"github.com/agentable/gotreesitter/internal/benchfixtures"
)

// TestAdmissionCandidateScorecard206 is the admission scorecard's missing half:
// it drives the compact candidate route across all 206 registered languages
// (one smoke fixture each) through the public Parse API and records, per
// language, whether the compact route served a byte-exact tree, declined and
// fell back, or diverged. The compact route had only ever been validated on the
// four canonical Go fixtures; this run reports how far it reaches.
//
// Scope: the per-language fixtures are trivial smoke snippets, so this is the
// breadth ratchet -- which grammars the compact route accepts at all -- rather
// than corpus-scale fidelity proof. Frozen canonical and representative-depth
// digests provide the deeper companion gate.
//
// It is a scorecard, not a gate: it never fails on a fallback (a fallback is the
// fail-closed, correct behavior for an unsupported grammar). It fails only on a
// DIVERGE — the compact route accepted a clean tree that disagrees with
// production — because a silent wrong tree is the one outcome the admission
// contract forbids. Set GTS_ADMISSION_SCORECARD_STRICT=1 to also fail if any
// DIVERGE is observed even when only logging is wanted otherwise.
//
// Statuses:
//
//   - PASS     candidate routed and its tree digest equals production's;
//   - DIVERGE  candidate routed but its tree digest differs from production;
//   - FALLBACK candidate declined; production served the parse (fail-closed);
//   - SKIP     language is not routable via the DFA Parse path (token source);
//   - ERROR    production itself failed or a panic was recovered.
const (
	scorecardPass     = "PASS"
	scorecardDiverge  = "DIVERGE"
	scorecardFallback = "FALLBACK"
	scorecardSkip     = "SKIP"
	scorecardError    = "ERROR"
)

type scorecardRow struct {
	name    string
	backend string
	status  string
	detail  string
}

// admissionScorecardRequiredCompactPasses is the frozen per-language admission
// manifest from the generalized clean-tail epoch. It intentionally lists only
// languages that the public compact route currently admits: a FALLBACK -> PASS
// improvement remains welcome, but a listed PASS -> FALLBACK/DIVERGE/ERROR is a
// release regression even when another language happens to improve and keeps
// the aggregate totals unchanged. This is a test-only release ratchet, never a
// runtime routing allowlist.
var admissionScorecardRequiredCompactPasses = map[string]struct{}{
	"ada": {}, "agda": {}, "angular": {}, "apex": {}, "arduino": {},
	"asm": {}, "astro": {}, "awk": {}, "bash": {}, "bass": {}, "beancount": {}, "bibtex": {},
	"bicep": {}, "bitbake": {}, "blade": {}, "brightscript": {}, "c_sharp": {},
	"caddy": {}, "cairo": {}, "capnp": {}, "chatito": {}, "circom": {},
	"clojure": {}, "cmake": {}, "comment": {}, "commonlisp": {}, "corn": {}, "cpon": {}, "crystal": {}, "css": {},
	"csv": {}, "cuda": {}, "cue": {}, "cylc": {}, "d": {}, "dart": {},
	"desktop": {}, "devicetree": {}, "dhall": {}, "diff": {}, "disassembly": {}, "djot": {}, "dockerfile": {},
	"dot": {}, "dtd": {}, "earthfile": {}, "ebnf": {}, "editorconfig": {},
	"eds": {}, "eex": {}, "elisp": {}, "elixir": {}, "elm": {}, "elsa": {}, "embedded_template": {}, "enforce": {},
	"erlang": {}, "facility": {}, "faust": {}, "fennel": {}, "fidl": {},
	"firrtl": {}, "fish": {}, "foam": {}, "forth": {}, "fortran": {}, "fsharp": {},
	"gdscript": {}, "git_config": {}, "git_rebase": {}, "gitattributes": {}, "gitcommit": {}, "gitignore": {}, "gleam": {},
	"glsl": {}, "gn": {}, "go": {}, "godot_resource": {}, "gomod": {}, "graphql": {},
	"groovy": {}, "hack": {}, "hare": {}, "haskell": {}, "haxe": {}, "hcl": {}, "heex": {},
	"hlsl": {}, "html": {}, "hurl": {}, "hyprlang": {}, "ini": {}, "janet": {}, "javascript": {}, "jinja2": {}, "jq": {},
	"json5": {}, "jsonnet": {}, "julia": {}, "just": {}, "kconfig": {}, "kdl": {}, "kotlin": {},
	"ledger": {}, "less": {}, "linkerscript": {}, "liquid": {}, "llvm": {}, "lua": {},
	"luau": {}, "make": {}, "markdown": {}, "matlab": {}, "mermaid": {}, "mojo": {},
	"move": {}, "nginx": {}, "nickel": {}, "nim": {}, "ninja": {}, "nix": {}, "norg": {}, "nushell": {},
	"objc": {}, "ocaml": {}, "odin": {}, "org": {}, "pascal": {}, "pem": {}, "perl": {},
	"php": {}, "pkl": {}, "powershell": {}, "prisma": {}, "prolog": {}, "promql": {},
	"properties": {}, "proto": {}, "pug": {}, "puppet": {}, "purescript": {}, "python": {}, "ql": {},
	"r": {}, "racket": {}, "regex": {}, "rego": {}, "requirements": {}, "rescript": {}, "ron": {},
	"rst": {}, "ruby": {}, "rust": {}, "scala": {}, "scheme": {}, "scss": {}, "smithy": {},
	"solidity": {}, "sparql": {}, "sql": {}, "squirrel": {}, "starlark": {}, "svelte": {},
	"ssh_config": {}, "swift": {}, "tablegen": {}, "tcl": {}, "teal": {}, "templ": {}, "textproto": {},
	"thrift": {}, "tlaplus": {}, "tmux": {}, "todotxt": {}, "toml": {}, "tsx": {}, "turtle": {}, "twig": {},
	"typescript": {}, "typst": {}, "uxntal": {}, "v": {}, "verilog": {}, "vimdoc": {},
	"vue": {}, "wat": {}, "wgsl": {}, "wolfram": {}, "xml": {}, "yaml": {}, "yuck": {}, "zig": {},
}

func TestAdmissionCandidateScorecard206(t *testing.T) {
	// This scorecard loads every registered grammar, which inflates process heap
	// enough to disturb the whole-process TestArenaGCRetentionAfterRelease gate.
	// It is a deliberate diagnostic, so gate it behind an explicit opt-in and
	// keep the routine suite unaffected. Run it with:
	//   GTS_ADMISSION_SCORECARD=1 go test -tags gts_parsercorephase0 \
	//     -run TestAdmissionCandidateScorecard206 -v .
	if os.Getenv("GTS_ADMISSION_SCORECARD") != "1" {
		t.Skip("set GTS_ADMISSION_SCORECARD=1 to run the 206-language admission scorecard")
	}
	// Purge the embedded grammar cache afterward so it does not inflate process
	// heap for later suite tests.
	t.Cleanup(func() { grammars.PurgeEmbeddedLanguageCache() })
	entries := grammars.AllLanguages()
	rows := make([]scorecardRow, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, runAdmissionScorecardLanguage(entry))
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].name < rows[j].name })

	counts := map[string]int{}
	var divergences []scorecardRow
	for _, row := range rows {
		counts[row.status]++
		if row.status == scorecardDiverge {
			divergences = append(divergences, row)
		}
	}

	t.Logf("=== Phase-3 admission candidate scorecard (%d languages) ===", len(rows))
	for _, row := range rows {
		t.Logf("%-9s %-16s %-6s %s", row.status, row.name, row.backend, row.detail)
	}
	t.Logf("--- summary: PASS=%d DIVERGE=%d FALLBACK=%d SKIP=%d ERROR=%d total=%d ---",
		counts[scorecardPass], counts[scorecardDiverge], counts[scorecardFallback],
		counts[scorecardSkip], counts[scorecardError], len(rows))

	if len(divergences) > 0 {
		for _, row := range divergences {
			t.Logf("DIVERGENCE FINDING: %s (%s) %s", row.name, row.backend, row.detail)
		}
		if os.Getenv("GTS_ADMISSION_SCORECARD_STRICT") == "1" {
			t.Fatalf("%d language(s) diverged through the compact route", len(divergences))
		}
	}

	if os.Getenv("GTS_ADMISSION_SCORECARD_RATCHET") == "1" {
		// Frozen at the generalized clean-tail admission epoch. Improvements are
		// welcome (more PASS, fewer FALLBACK); silent correctness failures,
		// registry drift, or surrendered route coverage require an explicit
		// review and ratchet update.
		const (
			wantTotal   = 206
			minPass     = 192
			maxFallback = 9
			wantSkip    = 5
		)
		if got := len(admissionScorecardRequiredCompactPasses); got != minPass {
			t.Fatalf("compact admission manifest has %d entries, want %d", got, minPass)
		}
		seen := make(map[string]struct{}, len(admissionScorecardRequiredCompactPasses))
		for _, row := range rows {
			if _, required := admissionScorecardRequiredCompactPasses[row.name]; !required {
				continue
			}
			seen[row.name] = struct{}{}
			if row.status != scorecardPass {
				t.Errorf("compact admission regression for %q: got %s (%s), want PASS", row.name, row.status, row.detail)
			}
		}
		for name := range admissionScorecardRequiredCompactPasses {
			if _, found := seen[name]; !found {
				t.Errorf("compact admission manifest language %q is missing from the scorecard", name)
			}
		}
		if t.Failed() {
			t.Fatal("per-language compact admission ratchet failed")
		}
		if len(rows) != wantTotal || counts[scorecardPass] < minPass ||
			counts[scorecardFallback] > maxFallback || counts[scorecardSkip] != wantSkip ||
			counts[scorecardDiverge] != 0 || counts[scorecardError] != 0 {
			t.Fatalf("admission breadth ratchet failed: PASS=%d (min %d) DIVERGE=%d FALLBACK=%d (max %d) SKIP=%d (want %d) ERROR=%d total=%d (want %d)",
				counts[scorecardPass], minPass, counts[scorecardDiverge], counts[scorecardFallback], maxFallback,
				counts[scorecardSkip], wantSkip, counts[scorecardError], len(rows), wantTotal)
		}
	}
}

// TestAdmissionSwitchRoutePrecedenceRatchet pins the three dispatch outcomes
// that release admission relies on. A certified forest policy owns the default
// route; an explicit compact request intentionally overrides it; with neither
// feature enabled, the original production route remains the only route.
func TestAdmissionSwitchRoutePrecedenceRatchet(t *testing.T) {
	previousDefault := gts.AdmissionCandidateRouteDefault()
	previousForest := os.Getenv("GOT_GLR_FOREST") != "0"
	t.Cleanup(func() {
		gts.SetAdmissionCandidateRouteDefault(previousDefault)
		gts.SetGLRForestEnabled(previousForest)
	})

	lang := grammars.AwkLanguage()
	if !lang.AutomaticForestEnabledByDefault {
		t.Fatal("awk must retain its exact-artifact certified forest profile")
	}
	source := []byte(grammars.ParseSmokeSample("awk"))

	t.Run("default follows certified forest", func(t *testing.T) {
		gts.SetAdmissionCandidateRouteDefault(true)
		gts.SetGLRForestEnabled(true)
		gts.ResetAdmissionCandidateCountersForTest()

		parser := gts.NewParser(lang) // no per-Parser override: forest keeps precedence
		tree, err := parser.Parse(source)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		defer tree.Release()
		if routed, fallback := gts.AdmissionCandidateCounters(); routed != 0 || fallback != 0 {
			t.Fatalf("default certified-forest parse consulted compact route: routed=%d fallback=%d", routed, fallback)
		}
		if !tree.ParseRuntime().ForestFastPath {
			_, _, reason, _ := parser.ForestDeclineInfo()
			if reason == "" {
				t.Fatal("default parser bypassed both the certified forest route and its recorded decline")
			}
		}
	})

	t.Run("explicit compact override wins", func(t *testing.T) {
		gts.SetAdmissionCandidateRouteDefault(true)
		gts.SetGLRForestEnabled(true)
		gts.ResetAdmissionCandidateCountersForTest()

		parser := gts.NewParser(lang)
		parser.SetAdmissionCandidateRoute(true)
		tree, err := parser.Parse(source)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		defer tree.Release()
		if routed, fallback := gts.AdmissionCandidateCounters(); routed != 1 || fallback != 0 {
			t.Fatalf("explicit compact override did not win: routed=%d fallback=%d", routed, fallback)
		}
	})

	t.Run("neither enabled remains production", func(t *testing.T) {
		gts.SetAdmissionCandidateRouteDefault(false)
		gts.SetGLRForestEnabled(false)
		gts.ResetAdmissionCandidateCountersForTest()

		parser := gts.NewParser(lang) // no compact override and no forest master switch
		tree, err := parser.Parse(source)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		defer tree.Release()
		if routed, fallback := gts.AdmissionCandidateCounters(); routed != 0 || fallback != 0 {
			t.Fatalf("production-only parse consulted compact route: routed=%d fallback=%d", routed, fallback)
		}
		if tree.ParseRuntime().ForestFastPath {
			t.Fatal("neither-enabled parse escaped production through the forest route")
		}
	})
}

func runAdmissionScorecardLanguage(entry grammars.LangEntry) (row scorecardRow) {
	row = scorecardRow{name: entry.Name, status: scorecardError}
	defer func() {
		if r := recover(); r != nil {
			row.status = scorecardError
			row.detail = fmt.Sprintf("panic: %v", r)
		}
	}()

	lang := entry.Language()
	if lang == nil {
		row.detail = "nil language"
		return row
	}
	support := grammars.EvaluateParseSupport(entry, lang)
	row.backend = string(support.Backend)

	// Only the fresh DFA Parse path is routable through the compact candidate.
	if support.Backend != grammars.ParseBackendDFA {
		row.status = scorecardSkip
		row.detail = "not DFA-routable: " + support.Reason
		return row
	}

	source := []byte(grammars.ParseSmokeSample(entry.Name))

	production := gts.NewParser(lang)
	production.SetAdmissionCandidateRoute(false)
	productionTree, err := production.Parse(source)
	if err != nil || productionTree == nil || productionTree.RootNode() == nil {
		row.detail = fmt.Sprintf("production parse failed: %v", err)
		return row
	}
	defer productionTree.Release()
	productionInspection, err := benchfixtures.InspectGoTree(productionTree.RootNode(), lang)
	if err != nil {
		row.detail = "production digest failed: " + err.Error()
		return row
	}

	candidate := gts.NewParser(lang)
	candidate.SetAdmissionCandidateRoute(true)
	gts.ResetAdmissionCandidateCountersForTest()
	candidateTree, err := candidate.Parse(source)
	if err != nil || candidateTree == nil {
		row.detail = fmt.Sprintf("candidate parse failed: %v", err)
		return row
	}
	defer candidateTree.Release()
	routed, _ := gts.AdmissionCandidateCounters()

	if routed == 0 {
		row.status = scorecardFallback
		row.detail = gts.AdmissionCandidateLastFallbackReason()
		return row
	}
	if candidateTree.RootNode() == nil {
		row.detail = "candidate routed but produced a nil root"
		return row
	}
	candidateInspection, err := benchfixtures.InspectGoTree(candidateTree.RootNode(), lang)
	if err != nil {
		row.detail = "candidate digest failed: " + err.Error()
		return row
	}
	if candidateInspection.SHA256 == productionInspection.SHA256 {
		row.status = scorecardPass
		row.detail = "digest " + candidateInspection.SHA256[:12]
		return row
	}
	row.status = scorecardDiverge
	row.detail = fmt.Sprintf("candidate=%s production=%s", candidateInspection.SHA256[:12], productionInspection.SHA256[:12])
	return row
}
