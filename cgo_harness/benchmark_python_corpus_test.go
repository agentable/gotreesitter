//go:build cgo && treesitter_c_bench_legacy

package cgoharness

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	gotreesitter "github.com/agentable/gotreesitter"
	"github.com/agentable/gotreesitter/grammars"
	sitterpython "github.com/smacker/go-tree-sitter/python"
)

const defaultPythonCorpusPath = "corpus_real/python/large__python3.8_grammar.py"

type pythonCorpusFile struct {
	path   string
	source []byte
}

type pythonCorpusParseMode string

const (
	pythonCorpusParseModeDFA                      pythonCorpusParseMode = "dfa"
	pythonCorpusParseModeDFANoCompat              pythonCorpusParseMode = "dfa_no_compat"
	pythonCorpusParseModeDFANoTree                pythonCorpusParseMode = "dfa_no_tree"
	pythonCorpusParseModeDFANoTreeWithCheckpoints pythonCorpusParseMode = "dfa_no_tree_with_checkpoints"
)

type pythonReduceChildPathStats struct {
	slicesAllocated   uint64
	slicesRetained    uint64
	slicesDropped     uint64
	pointersAllocated uint64
	pointersRetained  uint64
	pointersDropped   uint64
}

func (s *pythonReduceChildPathStats) add(rt gotreesitter.ReduceChildPathRuntime) {
	s.slicesAllocated += rt.SlicesAllocated
	s.slicesRetained += rt.SlicesRetained
	s.slicesDropped += rt.SlicesDropped
	s.pointersAllocated += rt.PointersAllocated
	s.pointersRetained += rt.PointersRetained
	s.pointersDropped += rt.PointersDropped
}

func (s pythonReduceChildPathStats) report(b *testing.B, tokens float64, name string) {
	b.ReportMetric(float64(s.slicesAllocated)/tokens, "reduce_child_slices_"+name+"_alloc/token")
	b.ReportMetric(float64(s.slicesRetained)/tokens, "reduce_child_slices_"+name+"_retained/token")
	b.ReportMetric(float64(s.slicesDropped)/tokens, "reduce_child_slices_"+name+"_dropped/token")
	b.ReportMetric(float64(s.pointersAllocated)/tokens, "reduce_child_ptrs_"+name+"_alloc/token")
	b.ReportMetric(float64(s.pointersRetained)/tokens, "reduce_child_ptrs_"+name+"_retained/token")
	b.ReportMetric(float64(s.pointersDropped)/tokens, "reduce_child_ptrs_"+name+"_dropped/token")
}

func reportPythonRuntimeDroppedMetric(b *testing.B, created, final uint64, tokens float64, name string) {
	if created < final {
		return
	}
	b.ReportMetric(float64(created-final)/tokens, name)
}

type pythonRuntimeBenchStats struct {
	ops                                  int
	arenaBreakdownSamples                int
	tokensConsumed                       uint64
	iterations                           uint64
	nodesAllocated                       uint64
	parentNodesAllocated                 uint64
	parentNodesRetained                  uint64
	parentNodesDropped                   uint64
	leafNodesAllocated                   uint64
	leafNodesRetained                    uint64
	leafNodesDropped                     uint64
	childSlicesAllocated                 uint64
	childSlicesRetained                  uint64
	childSlicesDropped                   uint64
	childPointersAllocated               uint64
	childPointersRetained                uint64
	childPointersDropped                 uint64
	reduceChildFastGSS                   pythonReduceChildPathStats
	reduceChildAllVisible                pythonReduceChildPathStats
	reduceChildScratchGeneral            pythonReduceChildPathStats
	reduceChildScratchNoAlias            pythonReduceChildPathStats
	transientChildSlicesAllocated        uint64
	transientChildPointersAllocated      uint64
	transientChildSlicesMaterialized     uint64
	transientChildPointersMaterialized   uint64
	transientParentNodesAllocated        uint64
	transientParentNodesMaterialized     uint64
	finalNodes                           uint64
	finalParentNodes                     uint64
	finalLeafNodes                       uint64
	finalFieldedParentNodes              uint64
	finalUnfieldedParentNodes            uint64
	finalVisibleParentNodes              uint64
	finalHiddenParentNodes               uint64
	finalCheckpointLeafNodes             uint64
	finalChildSlices                     uint64
	finalChildPointers                   uint64
	finalFieldIDElements                 uint64
	finalFieldSourceElements             uint64
	finalChildRefParents                 uint64
	finalChildRefs                       uint64
	finalChildRefMaterializedParents     uint64
	finalChildRefMaterializedChildren    uint64
	finalChildRefSingleChildAccesses     uint64
	finalChildRefSingleChildMaterialized uint64
	gssNodesAllocated                    uint64
	gssNodesRetained                     uint64
	gssNodesDropped                      uint64
	singleStackGSSNodes                  uint64
	multiStackGSSNodes                   uint64
	arenaBytesAllocated                  int64
	arenaNodeStructBytesAllocated        int64
	arenaNodeFieldMetadataBytesAllocated int64
	arenaNoTreeNodeBytesAllocated        int64
	arenaCompactFullLeafBytesAllocated   int64
	arenaPendingParentBytesAllocated     int64
	arenaPendingChildEntryBytesAllocated int64
	arenaFinalChildSidecarBytesAllocated int64
	arenaChildSliceBytesAllocated        int64
	arenaFieldIDBytesAllocated           int64
	arenaFieldSourceBytesAllocated       int64
	scratchBytesAllocated                int64
	entryScratchBytesAllocated           int64
	gssBytesAllocated                    int64
	mergeScratchBytesAllocated           int64
	externalCheckpointRecords            uint64
	externalCheckpointSlots              uint64
	externalCheckpointBytes              int64
	externalCheckpointSnapshotBytes      uint64
	externalCheckpointLeafNodes          uint64
	compactFullLeafCreated               uint64
	compactFullLeafMaterialized          uint64
	compactFullLeafMaterializedParent    uint64
	compactFullLeafParentRejects         gotreesitter.PendingParentRejectStats
	compactFullLeafFieldRejectPayloads   gotreesitter.PendingParentFieldRejectPayloadStats
	compactFullLeafMaterializedFinal     uint64
	compactFullLeafReasons               pythonMaterializeReasonStats
	compactFullLeafDropped               uint64
	pendingParentCreated                 uint64
	pendingParentMaterialized            uint64
	pendingParentMaterializedParent      uint64
	pendingParentParentRejects           gotreesitter.PendingParentRejectStats
	pendingParentFieldRejects            gotreesitter.PendingParentFieldRejectStats
	pendingParentFieldRejectPayloads     gotreesitter.PendingParentFieldRejectPayloadStats
	pendingParentMaterializedFinal       uint64
	pendingParentReasons                 pythonMaterializeReasonStats
	pendingParentDropped                 uint64
	pendingParentsFlattened              uint64
	pendingChildRefsFlattened            uint64
	pendingChildEntriesAllocated         uint64
	pendingChildEntryCapacity            uint64
	pendingChildEntryWaste               uint64
	pendingParentCandidates              uint64
	pendingParentRejects                 pythonPendingParentRejectStats
	preMatFieldRejectCandidates          uint64
	preMatFieldRejectSameKey             uint64
	preMatFieldRejectOverflow            uint64
	checkpointLeafFullNodesAvoided       uint64
	resultSelectionNanos                 int64
	transientParentMaterializeNanos      int64
	resultTreeBuildNanos                 int64
	transientChildMaterializeNanos       int64
	resultPythonKeywordRepairNanos       int64
	resultPythonRootRepairNanos          int64
	resultFinalizeRootNanos              int64
	resultExtendTrailingNanos            int64
	resultNormalizeRootStartNanos        int64
	resultCompatibilityNanos             int64
	resultParentLinkNanos                int64
	arenaNodesConstructed                uint64
	nodeLiveCount                        uint64
	nodeCapacityCount                    uint64
	nodeCapacityWaste                    uint64
	primaryNodeCapacity                  uint64
	primaryNodeUsed                      uint64
	overflowNodeCapacity                 uint64
	overflowNodeUsed                     uint64
	overflowNodeSlabs                    uint64
	largestNodeSlabUsedFractionSum       float64
	leafNodesConstructed                 uint64
	parentNodesConstructed               uint64
	fieldedParentNodesConstructed        uint64
	unfieldedParentNodesConstructed      uint64
	parentConstructedChildLen0           uint64
	parentConstructedChildLen1           uint64
	parentConstructedChildLen2           uint64
	parentConstructedChildLen3           uint64
	parentConstructedChildLen4Plus       uint64
	parentConstructedNoLinks             uint64
	parentConstructedWithLinks           uint64
	parentConstructedTrackErrors         uint64
	parentConstructedFieldSources        uint64
	parentReductionVisible               uint64
	parentReductionInvisible             uint64
	parentReductionVisibleFielded        uint64
	parentReductionVisibleUnfielded      uint64
	parentReductionInvisibleFielded      uint64
	parentReductionInvisibleUnfielded    uint64
	parentReductionVisibleChildPtrs      uint64
	parentReductionInvisibleChildPtrs    uint64
	parentReductionVisibleLen0           uint64
	parentReductionVisibleLen1           uint64
	parentReductionVisibleLen2           uint64
	parentReductionVisibleLen3           uint64
	parentReductionVisibleLen4Plus       uint64
	parentReductionInvisibleLen0         uint64
	parentReductionInvisibleLen1         uint64
	parentReductionInvisibleLen2         uint64
	parentReductionInvisibleLen3         uint64
	parentReductionInvisibleLen4Plus     uint64
	reduceChildSlicesFastGSS             uint64
	reduceChildPointersFastGSS           uint64
	reduceChildSlicesAllVisible          uint64
	reduceChildPointersAllVisible        uint64
	reduceChildSlicesScratchGeneral      uint64
	reduceChildPointersScratchGeneral    uint64
	reduceChildSlicesScratchNoAlias      uint64
	reduceChildPointersScratchNoAlias    uint64
	collapseRawUnaryAttempts             uint64
	collapseRawUnarySuccesses            uint64
	collapseRawUnaryMissShape            uint64
	collapseRawUnaryMissGrammar          uint64
	collapseRawUnaryMissChild            uint64
	collapseRawUnaryMissRule             uint64
	collapseUnaryAttempts                uint64
	collapseUnarySuccesses               uint64
	collapseUnaryMissShape               uint64
	collapseUnaryMissGrammar             uint64
	collapseUnaryMissFielded             uint64
	collapseUnaryMissChild               uint64
	collapseUnaryMissRule                uint64
	collapseRuleSameSymbol               uint64
	collapseRuleInvisibleWrapper         uint64
	collapseRuleNamedLeafAlias           uint64
	noTreeReduceNodesConstructed         uint64
	noTreeLeafNodesConstructed           uint64
	noTreePlaceholderNodesConstructed    uint64
	otherNodesConstructed                uint64
	extraNodesConstructed                uint64
	errorSymbolNodesConstructed          uint64
	hasErrorNodesConstructed             uint64
	childSlicesConstructed               uint64
	childPointersConstructed             uint64
	childSlicesLen1                      uint64
	childSlicesLen2                      uint64
	childSlicesLen3                      uint64
	childSlicesLen4Plus                  uint64
	parentChildPointersConstructed       uint64
	parentChildrenLen0                   uint64
	parentChildrenLen1                   uint64
	parentChildrenLen2                   uint64
	parentChildrenLen3                   uint64
	parentChildrenLen4Plus               uint64
	fieldIDElementsConstructed           uint64
	fieldSourceElementsConstructed       uint64
	normalizationPassesChecked           uint64
	normalizationPassesRun               uint64
	normalizationNodesVisited            uint64
	normalizationNodesRewritten          uint64
	normalizationNanos                   int64
	maxStacksSeen                        int
}

type pythonMaterializeReasonStats struct {
	normalization   uint64
	recovery        uint64
	query           uint64
	cursor          uint64
	parentAPI       uint64
	edit            uint64
	checkpointBuild uint64
}

type pythonPendingParentRejectStats struct {
	empty                  uint64
	childLimit             uint64
	alias                  uint64
	rawSpan                uint64
	fields                 uint64
	fieldsParentHide       uint64
	fieldsNoIDs            uint64
	fieldsInherited        uint64
	fieldsHidden           uint64
	fieldsHiddenPlain      uint64
	fieldsHiddenPlainEmpty uint64
	fieldsHiddenPlainOne   uint64
	fieldsHiddenPlainMany  uint64
	fieldsHiddenWithFields uint64
	fieldsChild            uint64
	fieldsAllVisible       uint64
	child                  uint64
	span                   uint64
	fill                   uint64
}

func addPendingParentRejectStats(dst *gotreesitter.PendingParentRejectStats, src gotreesitter.PendingParentRejectStats) {
	dst.Unknown += src.Unknown
	dst.Empty += src.Empty
	dst.ChildLimit += src.ChildLimit
	dst.Alias += src.Alias
	dst.RawSpan += src.RawSpan
	dst.Fields += src.Fields
	dst.Child += src.Child
	dst.Span += src.Span
	dst.Fill += src.Fill
}

func addPendingParentFieldRejectStats(dst *gotreesitter.PendingParentFieldRejectStats, src gotreesitter.PendingParentFieldRejectStats) {
	dst.Unknown += src.Unknown
	dst.ParentHidden += src.ParentHidden
	dst.NoIDs += src.NoIDs
	dst.Inherited += src.Inherited
	dst.HiddenChild += src.HiddenChild
	dst.HiddenChildPlain += src.HiddenChildPlain
	dst.HiddenChildPlainEmpty += src.HiddenChildPlainEmpty
	dst.HiddenChildPlainOne += src.HiddenChildPlainOne
	dst.HiddenChildPlainMany += src.HiddenChildPlainMany
	dst.HiddenChildWithFields += src.HiddenChildWithFields
	dst.Child += src.Child
	dst.AllVisibleDirect += src.AllVisibleDirect
}

func addPendingParentFieldRejectPayloadStats(dst *gotreesitter.PendingParentFieldRejectPayloadStats, src gotreesitter.PendingParentFieldRejectPayloadStats) {
	dst.Unknown += src.Unknown
	dst.Visible += src.Visible
	dst.VisibleFinalLike += src.VisibleFinalLike
	dst.VisibleNestedPayload += src.VisibleNestedPayload
	dst.VisibleCompactLeaf += src.VisibleCompactLeaf
	dst.VisibleFieldedDesc += src.VisibleFieldedDesc
	dst.HiddenEmpty += src.HiddenEmpty
	dst.HiddenOne += src.HiddenOne
	dst.HiddenMany += src.HiddenMany
	dst.HiddenWithFields += src.HiddenWithFields
}

func reportPendingParentRejectStats(b *testing.B, s gotreesitter.PendingParentRejectStats, tokens float64, prefix string) {
	b.ReportMetric(float64(s.Unknown)/tokens, prefix+"_unknown/token")
	b.ReportMetric(float64(s.Empty)/tokens, prefix+"_empty/token")
	b.ReportMetric(float64(s.ChildLimit)/tokens, prefix+"_child_limit/token")
	b.ReportMetric(float64(s.Alias)/tokens, prefix+"_alias/token")
	b.ReportMetric(float64(s.RawSpan)/tokens, prefix+"_raw_span/token")
	b.ReportMetric(float64(s.Fields)/tokens, prefix+"_fields/token")
	b.ReportMetric(float64(s.Child)/tokens, prefix+"_child/token")
	b.ReportMetric(float64(s.Span)/tokens, prefix+"_span/token")
	b.ReportMetric(float64(s.Fill)/tokens, prefix+"_fill/token")
}

func reportPendingParentFieldRejectStats(b *testing.B, s gotreesitter.PendingParentFieldRejectStats, tokens float64, prefix string) {
	b.ReportMetric(float64(s.Unknown)/tokens, prefix+"_unknown/token")
	b.ReportMetric(float64(s.ParentHidden)/tokens, prefix+"_parent_hidden/token")
	b.ReportMetric(float64(s.NoIDs)/tokens, prefix+"_no_ids/token")
	b.ReportMetric(float64(s.Inherited)/tokens, prefix+"_inherited/token")
	b.ReportMetric(float64(s.HiddenChild)/tokens, prefix+"_hidden_child/token")
	b.ReportMetric(float64(s.HiddenChildPlain)/tokens, prefix+"_hidden_child_plain/token")
	b.ReportMetric(float64(s.HiddenChildPlainEmpty)/tokens, prefix+"_hidden_child_plain_empty/token")
	b.ReportMetric(float64(s.HiddenChildPlainOne)/tokens, prefix+"_hidden_child_plain_one/token")
	b.ReportMetric(float64(s.HiddenChildPlainMany)/tokens, prefix+"_hidden_child_plain_many/token")
	b.ReportMetric(float64(s.HiddenChildWithFields)/tokens, prefix+"_hidden_child_with_fields/token")
	b.ReportMetric(float64(s.Child)/tokens, prefix+"_child/token")
	b.ReportMetric(float64(s.AllVisibleDirect)/tokens, prefix+"_all_visible_direct/token")
}

func reportPendingParentFieldRejectPayloadStats(b *testing.B, s gotreesitter.PendingParentFieldRejectPayloadStats, tokens float64, prefix string) {
	b.ReportMetric(float64(s.Unknown)/tokens, prefix+"_unknown/token")
	b.ReportMetric(float64(s.Visible)/tokens, prefix+"_visible/token")
	b.ReportMetric(float64(s.VisibleFinalLike)/tokens, prefix+"_visible_final_like/token")
	b.ReportMetric(float64(s.VisibleNestedPayload)/tokens, prefix+"_visible_nested_payload/token")
	b.ReportMetric(float64(s.VisibleCompactLeaf)/tokens, prefix+"_visible_compact_leaf/token")
	b.ReportMetric(float64(s.VisibleFieldedDesc)/tokens, prefix+"_visible_fielded_desc/token")
	b.ReportMetric(float64(s.HiddenEmpty)/tokens, prefix+"_hidden_empty/token")
	b.ReportMetric(float64(s.HiddenOne)/tokens, prefix+"_hidden_one/token")
	b.ReportMetric(float64(s.HiddenMany)/tokens, prefix+"_hidden_many/token")
	b.ReportMetric(float64(s.HiddenWithFields)/tokens, prefix+"_hidden_with_fields/token")
}

func (s *pythonRuntimeBenchStats) add(rt gotreesitter.ParseRuntime, breakdown gotreesitter.ArenaBreakdown, hasBreakdown bool) {
	s.ops++
	s.tokensConsumed += rt.TokensConsumed
	s.iterations += uint64(rt.Iterations)
	s.nodesAllocated += uint64(rt.NodesAllocated)
	s.parentNodesAllocated += rt.ParentNodesAllocated
	s.parentNodesRetained += rt.ParentNodesRetained
	s.parentNodesDropped += rt.ParentNodesDroppedSameToken
	s.leafNodesAllocated += rt.LeafNodesAllocated
	s.leafNodesRetained += rt.LeafNodesRetained
	s.leafNodesDropped += rt.LeafNodesDroppedSameToken
	s.childSlicesAllocated += rt.ChildSlicesAllocated
	s.childSlicesRetained += rt.ChildSlicesRetained
	s.childSlicesDropped += rt.ChildSlicesDroppedSameToken
	s.childPointersAllocated += rt.ChildPointersAllocated
	s.childPointersRetained += rt.ChildPointersRetained
	s.childPointersDropped += rt.ChildPointersDroppedSameToken
	s.reduceChildFastGSS.add(rt.ReduceChildFastGSS)
	s.reduceChildAllVisible.add(rt.ReduceChildAllVisible)
	s.reduceChildScratchGeneral.add(rt.ReduceChildScratchGeneral)
	s.reduceChildScratchNoAlias.add(rt.ReduceChildScratchNoAlias)
	s.transientChildSlicesAllocated += rt.TransientChildSlicesAllocated
	s.transientChildPointersAllocated += rt.TransientChildPointersAllocated
	s.transientChildSlicesMaterialized += rt.TransientChildSlicesMaterialized
	s.transientChildPointersMaterialized += rt.TransientChildPointersMaterialized
	s.transientParentNodesAllocated += rt.TransientParentNodesAllocated
	s.transientParentNodesMaterialized += rt.TransientParentNodesMaterialized
	s.finalNodes += rt.FinalNodes
	s.finalParentNodes += rt.FinalParentNodes
	s.finalLeafNodes += rt.FinalLeafNodes
	s.finalFieldedParentNodes += rt.FinalFieldedParentNodes
	s.finalUnfieldedParentNodes += rt.FinalUnfieldedParentNodes
	s.finalVisibleParentNodes += rt.FinalVisibleParentNodes
	s.finalHiddenParentNodes += rt.FinalHiddenParentNodes
	s.finalCheckpointLeafNodes += rt.FinalCheckpointLeafNodes
	s.finalChildSlices += rt.FinalChildSlices
	s.finalChildPointers += rt.FinalChildPointers
	s.finalFieldIDElements += rt.FinalFieldIDElements
	s.finalFieldSourceElements += rt.FinalFieldSourceElements
	s.finalChildRefParents += rt.FinalChildRefParents
	s.finalChildRefs += rt.FinalChildRefs
	s.finalChildRefMaterializedParents += rt.FinalChildRefMaterializedParents
	s.finalChildRefMaterializedChildren += rt.FinalChildRefMaterializedChildren
	s.finalChildRefSingleChildAccesses += rt.FinalChildRefSingleChildAccesses
	s.finalChildRefSingleChildMaterialized += rt.FinalChildRefSingleChildMaterializedChildren
	s.gssNodesAllocated += rt.GSSNodesAllocated
	s.gssNodesRetained += rt.GSSNodesRetained
	s.gssNodesDropped += rt.GSSNodesDroppedSameToken
	s.singleStackGSSNodes += rt.SingleStackGSSNodes
	s.multiStackGSSNodes += rt.MultiStackGSSNodes
	s.arenaBytesAllocated += rt.ArenaBytesAllocated
	s.scratchBytesAllocated += rt.ScratchBytesAllocated
	s.entryScratchBytesAllocated += rt.EntryScratchBytesAllocated
	s.gssBytesAllocated += rt.GSSBytesAllocated
	s.externalCheckpointRecords += rt.ExternalScannerCheckpointRecords
	s.externalCheckpointSlots += rt.ExternalScannerCheckpointSlotsAllocated
	s.externalCheckpointBytes += rt.ExternalScannerCheckpointBytesAllocated
	s.externalCheckpointSnapshotBytes += rt.ExternalScannerSnapshotBytesAllocated
	s.externalCheckpointLeafNodes += rt.ExternalScannerCheckpointLeafNodes
	s.compactFullLeafCreated += rt.CompactFullLeafCreated
	s.compactFullLeafMaterialized += rt.CompactFullLeafMaterialized
	s.compactFullLeafMaterializedParent += rt.CompactFullLeafMaterializedForParentReduce
	addPendingParentFieldRejectPayloadStats(&s.compactFullLeafFieldRejectPayloads, rt.CompactFullLeafMaterializedForFieldRejectPayload)
	s.compactFullLeafMaterializedFinal += rt.CompactFullLeafMaterializedForFinalTree
	s.compactFullLeafReasons.normalization += rt.CompactFullLeafMaterializedForNormalization
	s.compactFullLeafReasons.recovery += rt.CompactFullLeafMaterializedForRecovery
	s.compactFullLeafReasons.query += rt.CompactFullLeafMaterializedForQuery
	s.compactFullLeafReasons.cursor += rt.CompactFullLeafMaterializedForCursor
	s.compactFullLeafReasons.parentAPI += rt.CompactFullLeafMaterializedForParentAPI
	s.compactFullLeafReasons.edit += rt.CompactFullLeafMaterializedForEdit
	s.compactFullLeafReasons.checkpointBuild += rt.CompactFullLeafMaterializedForCheckpointRebuild
	s.compactFullLeafDropped += rt.CompactFullLeafDropped
	s.pendingParentCreated += rt.PendingParentCreated
	s.pendingParentMaterialized += rt.PendingParentMaterialized
	s.pendingParentMaterializedParent += rt.PendingParentMaterializedForParentReduce
	addPendingParentRejectStats(&s.compactFullLeafParentRejects, rt.CompactFullLeafMaterializedForParentReject)
	addPendingParentRejectStats(&s.pendingParentParentRejects, rt.PendingParentMaterializedForParentReject)
	addPendingParentFieldRejectStats(&s.pendingParentFieldRejects, rt.PendingParentMaterializedForFieldReject)
	addPendingParentFieldRejectPayloadStats(&s.pendingParentFieldRejectPayloads, rt.PendingParentMaterializedForFieldRejectPayload)
	s.pendingParentMaterializedFinal += rt.PendingParentMaterializedForFinalTree
	s.pendingParentReasons.normalization += rt.PendingParentMaterializedForNormalization
	s.pendingParentReasons.recovery += rt.PendingParentMaterializedForRecovery
	s.pendingParentReasons.query += rt.PendingParentMaterializedForQuery
	s.pendingParentReasons.cursor += rt.PendingParentMaterializedForCursor
	s.pendingParentReasons.parentAPI += rt.PendingParentMaterializedForParentAPI
	s.pendingParentReasons.edit += rt.PendingParentMaterializedForEdit
	s.pendingParentReasons.checkpointBuild += rt.PendingParentMaterializedForCheckpointRebuild
	s.pendingParentDropped += rt.PendingParentDropped
	s.pendingParentsFlattened += rt.PendingParentsFlattened
	s.pendingChildRefsFlattened += rt.PendingChildRefsFlattened
	s.pendingChildEntriesAllocated += rt.PendingChildEntriesAllocated
	s.pendingChildEntryCapacity += rt.PendingChildEntryCapacity
	s.pendingChildEntryWaste += rt.PendingChildEntryWaste
	s.pendingParentCandidates += rt.PendingParentCandidates
	s.preMatFieldRejectCandidates += rt.PreMaterializationFieldRejectCandidates
	s.preMatFieldRejectSameKey += rt.PreMaterializationFieldRejectSameKeyCandidates
	s.preMatFieldRejectOverflow += rt.PreMaterializationFieldRejectOverflowCandidates
	s.pendingParentRejects.empty += rt.PendingParentRejectedEmpty
	s.pendingParentRejects.childLimit += rt.PendingParentRejectedChildLimit
	s.pendingParentRejects.alias += rt.PendingParentRejectedAlias
	s.pendingParentRejects.rawSpan += rt.PendingParentRejectedRawSpan
	s.pendingParentRejects.fields += rt.PendingParentRejectedFields
	s.pendingParentRejects.fieldsParentHide += rt.PendingParentRejectedFieldsParentHidden
	s.pendingParentRejects.fieldsNoIDs += rt.PendingParentRejectedFieldsNoIDs
	s.pendingParentRejects.fieldsInherited += rt.PendingParentRejectedFieldsInherited
	s.pendingParentRejects.fieldsHidden += rt.PendingParentRejectedFieldsHiddenChild
	s.pendingParentRejects.fieldsHiddenPlain += rt.PendingParentRejectedFieldsHiddenChildPlain
	s.pendingParentRejects.fieldsHiddenPlainEmpty += rt.PendingParentRejectedFieldsHiddenChildPlainEmpty
	s.pendingParentRejects.fieldsHiddenPlainOne += rt.PendingParentRejectedFieldsHiddenChildPlainOne
	s.pendingParentRejects.fieldsHiddenPlainMany += rt.PendingParentRejectedFieldsHiddenChildPlainMany
	s.pendingParentRejects.fieldsHiddenWithFields += rt.PendingParentRejectedFieldsHiddenChildWithFields
	s.pendingParentRejects.fieldsChild += rt.PendingParentRejectedFieldsChild
	s.pendingParentRejects.fieldsAllVisible += rt.PendingParentRejectedFieldsAllVisibleDirect
	s.pendingParentRejects.child += rt.PendingParentRejectedChild
	s.pendingParentRejects.span += rt.PendingParentRejectedSpan
	s.pendingParentRejects.fill += rt.PendingParentRejectedFill
	s.checkpointLeafFullNodesAvoided += rt.CheckpointLeafFullNodesAvoided
	s.resultSelectionNanos += rt.ResultSelectionNanos
	s.transientParentMaterializeNanos += rt.TransientParentMaterializationNanos
	s.resultTreeBuildNanos += rt.ResultTreeBuildNanos
	s.transientChildMaterializeNanos += rt.TransientChildMaterializationNanos
	s.resultPythonKeywordRepairNanos += rt.ResultPythonKeywordRepairNanos
	s.resultPythonRootRepairNanos += rt.ResultPythonRootRepairNanos
	s.resultFinalizeRootNanos += rt.ResultFinalizeRootNanos
	s.resultExtendTrailingNanos += rt.ResultExtendTrailingNanos
	s.resultNormalizeRootStartNanos += rt.ResultNormalizeRootStartNanos
	s.resultCompatibilityNanos += rt.ResultCompatibilityNanos
	s.resultParentLinkNanos += rt.ResultParentLinkNanos
	s.leafNodesConstructed += rt.LeafNodesConstructed
	s.parentNodesConstructed += rt.ParentNodesConstructed
	s.noTreeReduceNodesConstructed += rt.NoTreeReduceNodesConstructed
	s.noTreeLeafNodesConstructed += rt.NoTreeLeafNodesConstructed
	if hasBreakdown {
		s.arenaBreakdownSamples++
		s.arenaNodeStructBytesAllocated += breakdown.NodeStructBytesAllocated
		s.arenaNodeFieldMetadataBytesAllocated += breakdown.NodeFieldMetadataBytesAllocated
		s.arenaNoTreeNodeBytesAllocated += breakdown.NoTreeNodeBytesAllocated
		s.arenaCompactFullLeafBytesAllocated += breakdown.CompactFullLeafBytesAllocated
		s.arenaPendingParentBytesAllocated += breakdown.PendingParentBytesAllocated
		s.arenaPendingChildEntryBytesAllocated += breakdown.PendingChildEntryBytesAllocated
		s.arenaFinalChildSidecarBytesAllocated += breakdown.FinalChildSidecarBytesAllocated
		s.arenaChildSliceBytesAllocated += breakdown.ChildSliceBytesAllocated
		s.arenaFieldIDBytesAllocated += breakdown.FieldIDBytesAllocated
		s.arenaFieldSourceBytesAllocated += breakdown.FieldSourceBytesAllocated
		s.mergeScratchBytesAllocated += breakdown.MergeScratchBytesAllocated
		s.arenaNodesConstructed += breakdown.ArenaNodesConstructed
		s.nodeLiveCount += breakdown.NodeLiveCount
		s.nodeCapacityCount += breakdown.NodeCapacityCount
		s.nodeCapacityWaste += breakdown.NodeCapacityWaste
		s.primaryNodeCapacity += breakdown.PrimaryNodeCapacity
		s.primaryNodeUsed += breakdown.PrimaryNodeUsed
		s.overflowNodeCapacity += breakdown.OverflowNodeCapacity
		s.overflowNodeUsed += breakdown.OverflowNodeUsed
		s.overflowNodeSlabs += breakdown.OverflowNodeSlabs
		s.largestNodeSlabUsedFractionSum += breakdown.LargestNodeSlabUsedFraction
		s.fieldedParentNodesConstructed += breakdown.FieldedParentNodesConstructed
		s.unfieldedParentNodesConstructed += breakdown.UnfieldedParentNodesConstructed
		s.parentConstructedChildLen0 += breakdown.ParentConstructedChildLen0
		s.parentConstructedChildLen1 += breakdown.ParentConstructedChildLen1
		s.parentConstructedChildLen2 += breakdown.ParentConstructedChildLen2
		s.parentConstructedChildLen3 += breakdown.ParentConstructedChildLen3
		s.parentConstructedChildLen4Plus += breakdown.ParentConstructedChildLen4Plus
		s.parentConstructedNoLinks += breakdown.ParentConstructedNoLinks
		s.parentConstructedWithLinks += breakdown.ParentConstructedWithLinks
		s.parentConstructedTrackErrors += breakdown.ParentConstructedTrackErrors
		s.parentConstructedFieldSources += breakdown.ParentConstructedFieldSources
		s.parentReductionVisible += breakdown.ParentReductionVisible
		s.parentReductionInvisible += breakdown.ParentReductionInvisible
		s.parentReductionVisibleFielded += breakdown.ParentReductionVisibleFielded
		s.parentReductionVisibleUnfielded += breakdown.ParentReductionVisibleUnfielded
		s.parentReductionInvisibleFielded += breakdown.ParentReductionInvisibleFielded
		s.parentReductionInvisibleUnfielded += breakdown.ParentReductionInvisibleUnfielded
		s.parentReductionVisibleChildPtrs += breakdown.ParentReductionVisibleChildPtrs
		s.parentReductionInvisibleChildPtrs += breakdown.ParentReductionInvisibleChildPtrs
		s.parentReductionVisibleLen0 += breakdown.ParentReductionVisibleLen0
		s.parentReductionVisibleLen1 += breakdown.ParentReductionVisibleLen1
		s.parentReductionVisibleLen2 += breakdown.ParentReductionVisibleLen2
		s.parentReductionVisibleLen3 += breakdown.ParentReductionVisibleLen3
		s.parentReductionVisibleLen4Plus += breakdown.ParentReductionVisibleLen4Plus
		s.parentReductionInvisibleLen0 += breakdown.ParentReductionInvisibleLen0
		s.parentReductionInvisibleLen1 += breakdown.ParentReductionInvisibleLen1
		s.parentReductionInvisibleLen2 += breakdown.ParentReductionInvisibleLen2
		s.parentReductionInvisibleLen3 += breakdown.ParentReductionInvisibleLen3
		s.parentReductionInvisibleLen4Plus += breakdown.ParentReductionInvisibleLen4Plus
		s.reduceChildSlicesFastGSS += breakdown.ReduceChildSlicesFastGSS
		s.reduceChildPointersFastGSS += breakdown.ReduceChildPointersFastGSS
		s.reduceChildSlicesAllVisible += breakdown.ReduceChildSlicesAllVisible
		s.reduceChildPointersAllVisible += breakdown.ReduceChildPointersAllVisible
		s.reduceChildSlicesScratchGeneral += breakdown.ReduceChildSlicesScratchGeneral
		s.reduceChildPointersScratchGeneral += breakdown.ReduceChildPointersScratchGeneral
		s.reduceChildSlicesScratchNoAlias += breakdown.ReduceChildSlicesScratchNoAlias
		s.reduceChildPointersScratchNoAlias += breakdown.ReduceChildPointersScratchNoAlias
		s.collapseRawUnaryAttempts += breakdown.CollapseRawUnaryAttempts
		s.collapseRawUnarySuccesses += breakdown.CollapseRawUnarySuccesses
		s.collapseRawUnaryMissShape += breakdown.CollapseRawUnaryMissShape
		s.collapseRawUnaryMissGrammar += breakdown.CollapseRawUnaryMissGrammar
		s.collapseRawUnaryMissChild += breakdown.CollapseRawUnaryMissChild
		s.collapseRawUnaryMissRule += breakdown.CollapseRawUnaryMissRule
		s.collapseUnaryAttempts += breakdown.CollapseUnaryAttempts
		s.collapseUnarySuccesses += breakdown.CollapseUnarySuccesses
		s.collapseUnaryMissShape += breakdown.CollapseUnaryMissShape
		s.collapseUnaryMissGrammar += breakdown.CollapseUnaryMissGrammar
		s.collapseUnaryMissFielded += breakdown.CollapseUnaryMissFielded
		s.collapseUnaryMissChild += breakdown.CollapseUnaryMissChild
		s.collapseUnaryMissRule += breakdown.CollapseUnaryMissRule
		s.collapseRuleSameSymbol += breakdown.CollapseRuleSameSymbol
		s.collapseRuleInvisibleWrapper += breakdown.CollapseRuleInvisibleWrapper
		s.collapseRuleNamedLeafAlias += breakdown.CollapseRuleNamedLeafAlias
		s.noTreePlaceholderNodesConstructed += breakdown.NoTreePlaceholderNodesConstructed
		s.otherNodesConstructed += breakdown.OtherNodesConstructed
		s.extraNodesConstructed += breakdown.ExtraNodesConstructed
		s.errorSymbolNodesConstructed += breakdown.ErrorSymbolNodesConstructed
		s.hasErrorNodesConstructed += breakdown.HasErrorNodesConstructed
		s.childSlicesConstructed += breakdown.ChildSlicesConstructed
		s.childPointersConstructed += breakdown.ChildPointersConstructed
		s.childSlicesLen1 += breakdown.ChildSlicesLen1
		s.childSlicesLen2 += breakdown.ChildSlicesLen2
		s.childSlicesLen3 += breakdown.ChildSlicesLen3
		s.childSlicesLen4Plus += breakdown.ChildSlicesLen4Plus
		s.parentChildPointersConstructed += breakdown.ParentChildPointersConstructed
		s.parentChildrenLen0 += breakdown.ParentChildrenLen0
		s.parentChildrenLen1 += breakdown.ParentChildrenLen1
		s.parentChildrenLen2 += breakdown.ParentChildrenLen2
		s.parentChildrenLen3 += breakdown.ParentChildrenLen3
		s.parentChildrenLen4Plus += breakdown.ParentChildrenLen4Plus
		s.fieldIDElementsConstructed += breakdown.FieldIDElementsConstructed
		s.fieldSourceElementsConstructed += breakdown.FieldSourceElementsConstructed
	}
	s.normalizationPassesChecked += rt.NormalizationPassesChecked
	s.normalizationPassesRun += rt.NormalizationPassesRun
	s.normalizationNodesVisited += rt.NormalizationNodesVisited
	s.normalizationNodesRewritten += rt.NormalizationNodesRewritten
	s.normalizationNanos += rt.NormalizationNanos
	if rt.MaxStacksSeen > s.maxStacksSeen {
		s.maxStacksSeen = rt.MaxStacksSeen
	}
}

func (s pythonRuntimeBenchStats) report(b *testing.B) {
	if s.ops == 0 {
		return
	}
	b.ReportMetric(float64(s.tokensConsumed)/float64(s.ops), "tokens/op")
	b.ReportMetric(float64(s.maxStacksSeen), "max_stacks")
	b.ReportMetric(float64(s.normalizationNanos)/float64(s.ops), "norm_ns/op")
	b.ReportMetric(float64(s.normalizationPassesChecked)/float64(s.ops), "norm_checked/op")
	b.ReportMetric(float64(s.normalizationPassesRun)/float64(s.ops), "norm_runs/op")
	if s.tokensConsumed == 0 {
		return
	}
	tokens := float64(s.tokensConsumed)
	gssNodes := s.gssNodesAllocated
	if gssNodes == 0 {
		gssNodes = s.singleStackGSSNodes + s.multiStackGSSNodes
	}
	b.ReportMetric(float64(s.iterations)/tokens, "iters/token")
	b.ReportMetric(float64(s.nodesAllocated)/tokens, "nodes/token")
	b.ReportMetric(float64(s.leafNodesConstructed)/tokens, "leaf_nodes/token")
	b.ReportMetric(float64(s.leafNodesConstructed)/tokens, "leaf_full_nodes/token")
	b.ReportMetric(float64(s.parentNodesConstructed)/tokens, "parent_nodes/token")
	b.ReportMetric(float64(s.parentNodesConstructed)/tokens, "parent_full_nodes/token")
	b.ReportMetric(float64(s.noTreeReduceNodesConstructed)/tokens, "notree_nodes/token")
	b.ReportMetric(float64(s.noTreeLeafNodesConstructed)/tokens, "notree_leaf_nodes/token")
	if s.finalChildRefParents != 0 || s.finalChildRefs != 0 || s.finalChildRefMaterializedParents != 0 || s.finalChildRefSingleChildAccesses != 0 {
		b.ReportMetric(float64(s.finalChildRefParents)/tokens, "final_child_ref_parents/token")
		b.ReportMetric(float64(s.finalChildRefs)/tokens, "final_child_refs/token")
		b.ReportMetric(float64(s.finalChildRefMaterializedParents)/tokens, "final_child_ref_range_materialized_parents/token")
		b.ReportMetric(float64(s.finalChildRefMaterializedChildren)/tokens, "final_child_ref_range_materialized_children/token")
		b.ReportMetric(float64(s.finalChildRefSingleChildAccesses)/tokens, "final_child_ref_single_child_accesses/token")
		b.ReportMetric(float64(s.finalChildRefSingleChildMaterialized)/tokens, "final_child_ref_single_child_materialized/token")
	}
	if s.finalNodes != 0 {
		b.ReportMetric(float64(s.finalNodes)/tokens, "final_nodes/token")
		b.ReportMetric(float64(s.finalParentNodes)/tokens, "final_parent_nodes/token")
		b.ReportMetric(float64(s.finalLeafNodes)/tokens, "final_leaf_nodes/token")
		b.ReportMetric(float64(s.finalFieldedParentNodes)/tokens, "final_fielded_parent_nodes/token")
		b.ReportMetric(float64(s.finalUnfieldedParentNodes)/tokens, "final_unfielded_parent_nodes/token")
		b.ReportMetric(float64(s.finalVisibleParentNodes)/tokens, "final_visible_parent_nodes/token")
		b.ReportMetric(float64(s.finalHiddenParentNodes)/tokens, "final_hidden_parent_nodes/token")
		b.ReportMetric(float64(s.finalCheckpointLeafNodes)/tokens, "final_checkpoint_leaf_nodes/token")
		b.ReportMetric(float64(s.finalChildSlices)/tokens, "final_child_slices/token")
		b.ReportMetric(float64(s.finalChildPointers)/tokens, "final_child_ptrs/token")
		b.ReportMetric(float64(s.finalFieldIDElements)/tokens, "final_field_ids/token")
		b.ReportMetric(float64(s.finalFieldSourceElements)/tokens, "final_field_sources/token")
		b.ReportMetric(float64(s.leafNodesConstructed)/tokens, "full_leaf_nodes_created/token")
		b.ReportMetric(float64(s.finalLeafNodes)/tokens, "full_leaf_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.leafNodesConstructed, s.finalLeafNodes, tokens, "full_leaf_nodes_dropped/token")
		b.ReportMetric(float64(s.parentNodesConstructed)/tokens, "parent_nodes_created/token")
		b.ReportMetric(float64(s.finalParentNodes)/tokens, "parent_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.parentNodesConstructed, s.finalParentNodes, tokens, "parent_nodes_dropped/token")
		b.ReportMetric(float64(s.externalCheckpointLeafNodes)/tokens, "checkpoint_leaf_nodes_created/token")
		b.ReportMetric(float64(s.finalCheckpointLeafNodes)/tokens, "checkpoint_leaf_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.externalCheckpointLeafNodes, s.finalCheckpointLeafNodes, tokens, "checkpoint_leaf_nodes_dropped/token")
	}
	if s.parentNodesAllocated != 0 || s.leafNodesAllocated != 0 {
		b.ReportMetric(float64(s.parentNodesAllocated)/tokens, "surv_parent_nodes/token")
		b.ReportMetric(float64(s.leafNodesAllocated)/tokens, "surv_leaf_nodes/token")
		fullNodesAllocated := s.parentNodesAllocated + s.leafNodesAllocated
		fullNodesRetained := s.parentNodesRetained + s.leafNodesRetained
		fullNodesDropped := s.parentNodesDropped + s.leafNodesDropped
		b.ReportMetric(float64(fullNodesAllocated)/tokens, "full_nodes_alloc/token")
		b.ReportMetric(float64(fullNodesRetained)/tokens, "full_nodes_retained/token")
		b.ReportMetric(float64(fullNodesRetained)/tokens, "nodes_retained/token")
		b.ReportMetric(float64(fullNodesDropped)/tokens, "full_nodes_dropped/token")
		b.ReportMetric(float64(fullNodesDropped)/tokens, "nodes_discarded/token")
		b.ReportMetric(float64(s.parentNodesRetained)/tokens, "parent_retained/token")
		b.ReportMetric(float64(s.parentNodesDropped)/tokens, "parent_dropped/token")
		b.ReportMetric(float64(s.leafNodesRetained)/tokens, "leaf_retained/token")
		b.ReportMetric(float64(s.leafNodesDropped)/tokens, "leaf_dropped/token")
		b.ReportMetric(float64(s.childSlicesAllocated)/tokens, "child_slices_alloc/token")
		b.ReportMetric(float64(s.childSlicesRetained)/tokens, "child_slices_retained/token")
		b.ReportMetric(float64(s.childSlicesDropped)/tokens, "child_slices_dropped/token")
		b.ReportMetric(float64(s.childSlicesDropped)/tokens, "child_slices_discarded/token")
		b.ReportMetric(float64(s.childPointersAllocated)/tokens, "child_ptrs_alloc/token")
		b.ReportMetric(float64(s.childPointersRetained)/tokens, "child_ptrs_retained/token")
		b.ReportMetric(float64(s.childPointersDropped)/tokens, "child_ptrs_dropped/token")
		b.ReportMetric(float64(s.childPointersDropped)/tokens, "child_ptrs_discarded/token")
		s.reduceChildFastGSS.report(b, tokens, "fast_gss")
		s.reduceChildAllVisible.report(b, tokens, "all_visible")
		s.reduceChildScratchGeneral.report(b, tokens, "scratch_general")
		s.reduceChildScratchNoAlias.report(b, tokens, "scratch_no_alias")
	}
	if s.transientChildSlicesAllocated != 0 || s.transientChildSlicesMaterialized != 0 {
		b.ReportMetric(float64(s.transientChildSlicesAllocated)/tokens, "transient_child_slices_alloc/token")
		b.ReportMetric(float64(s.transientChildPointersAllocated)/tokens, "transient_child_ptrs_alloc/token")
		b.ReportMetric(float64(s.transientChildSlicesMaterialized)/tokens, "transient_child_slices_materialized/token")
		b.ReportMetric(float64(s.transientChildPointersMaterialized)/tokens, "transient_child_ptrs_materialized/token")
		b.ReportMetric(float64(s.transientParentNodesAllocated)/tokens, "transient_parent_nodes_alloc/token")
		b.ReportMetric(float64(s.transientParentNodesMaterialized)/tokens, "transient_parent_nodes_materialized/token")
		if s.transientParentNodesAllocated >= s.transientParentNodesMaterialized {
			b.ReportMetric(float64(s.transientParentNodesAllocated-s.transientParentNodesMaterialized)/tokens, "transient_parent_nodes_dropped/token")
		}
	}
	if s.gssNodesRetained != 0 || s.gssNodesDropped != 0 {
		b.ReportMetric(float64(s.gssNodesRetained)/tokens, "gss_retained/token")
		b.ReportMetric(float64(s.gssNodesDropped)/tokens, "gss_dropped/token")
	}
	b.ReportMetric(float64(gssNodes)/tokens, "gss_nodes/token")
	b.ReportMetric(float64(s.singleStackGSSNodes)/tokens, "single_gss/token")
	b.ReportMetric(float64(s.multiStackGSSNodes)/tokens, "multi_gss/token")
	b.ReportMetric(float64(s.arenaBytesAllocated)/tokens, "arena_B/token")
	b.ReportMetric(float64(s.scratchBytesAllocated)/tokens, "scratch_B/token")
	b.ReportMetric(float64(s.entryScratchBytesAllocated)/tokens, "entry_B/token")
	b.ReportMetric(float64(s.gssBytesAllocated)/tokens, "gss_B/token")
	if s.arenaBreakdownSamples != 0 {
		b.ReportMetric(float64(s.arenaNodesConstructed)/tokens, "arena_nodes/token")
		b.ReportMetric(float64(s.nodeLiveCount)/tokens, "node_live/token")
		b.ReportMetric(float64(s.nodeCapacityCount)/tokens, "node_capacity/token")
		b.ReportMetric(float64(s.nodeCapacityWaste)/tokens, "node_capacity_waste/token")
		b.ReportMetric(float64(s.primaryNodeCapacity)/float64(s.arenaBreakdownSamples), "primary_node_capacity")
		b.ReportMetric(float64(s.primaryNodeUsed)/float64(s.arenaBreakdownSamples), "primary_node_used")
		b.ReportMetric(float64(s.overflowNodeCapacity)/float64(s.arenaBreakdownSamples), "overflow_node_capacity")
		b.ReportMetric(float64(s.overflowNodeUsed)/float64(s.arenaBreakdownSamples), "overflow_node_used")
		b.ReportMetric(float64(s.overflowNodeSlabs)/float64(s.arenaBreakdownSamples), "overflow_slabs")
		b.ReportMetric(s.largestNodeSlabUsedFractionSum/float64(s.arenaBreakdownSamples), "largest_slab_used_fraction")
		b.ReportMetric(float64(s.fieldedParentNodesConstructed)/tokens, "fielded_parent_nodes/token")
		b.ReportMetric(float64(s.unfieldedParentNodesConstructed)/tokens, "unfielded_parent_nodes/token")
		b.ReportMetric(float64(s.fieldedParentNodesConstructed)/tokens, "fielded_parent_nodes_created/token")
		b.ReportMetric(float64(s.finalFieldedParentNodes)/tokens, "fielded_parent_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.fieldedParentNodesConstructed, s.finalFieldedParentNodes, tokens, "fielded_parent_nodes_dropped/token")
		b.ReportMetric(float64(s.unfieldedParentNodesConstructed)/tokens, "unfielded_parent_nodes_created/token")
		b.ReportMetric(float64(s.finalUnfieldedParentNodes)/tokens, "unfielded_parent_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.unfieldedParentNodesConstructed, s.finalUnfieldedParentNodes, tokens, "unfielded_parent_nodes_dropped/token")
		b.ReportMetric(float64(s.parentReductionVisible)/tokens, "visible_parent_nodes_created/token")
		b.ReportMetric(float64(s.finalVisibleParentNodes)/tokens, "visible_parent_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.parentReductionVisible, s.finalVisibleParentNodes, tokens, "visible_parent_nodes_dropped/token")
		b.ReportMetric(float64(s.parentReductionInvisible)/tokens, "hidden_parent_nodes_created/token")
		b.ReportMetric(float64(s.finalHiddenParentNodes)/tokens, "hidden_parent_nodes_final/token")
		reportPythonRuntimeDroppedMetric(b, s.parentReductionInvisible, s.finalHiddenParentNodes, tokens, "hidden_parent_nodes_dropped/token")
		b.ReportMetric(float64(s.parentConstructedChildLen0)/tokens, "parent_ctor_len0/token")
		b.ReportMetric(float64(s.parentConstructedChildLen1)/tokens, "parent_ctor_len1/token")
		b.ReportMetric(float64(s.parentConstructedChildLen2)/tokens, "parent_ctor_len2/token")
		b.ReportMetric(float64(s.parentConstructedChildLen3)/tokens, "parent_ctor_len3/token")
		b.ReportMetric(float64(s.parentConstructedChildLen4Plus)/tokens, "parent_ctor_len4plus/token")
		b.ReportMetric(float64(s.parentConstructedNoLinks)/tokens, "parent_ctor_nolinks/token")
		b.ReportMetric(float64(s.parentConstructedWithLinks)/tokens, "parent_ctor_withlinks/token")
		b.ReportMetric(float64(s.parentConstructedTrackErrors)/tokens, "parent_ctor_track_errors/token")
		b.ReportMetric(float64(s.parentConstructedFieldSources)/tokens, "parent_ctor_field_sources/token")
		b.ReportMetric(float64(s.parentReductionVisible)/tokens, "parent_reduce_visible/token")
		b.ReportMetric(float64(s.parentReductionInvisible)/tokens, "parent_reduce_invisible/token")
		b.ReportMetric(float64(s.parentReductionVisibleFielded)/tokens, "parent_reduce_visible_fielded/token")
		b.ReportMetric(float64(s.parentReductionVisibleUnfielded)/tokens, "parent_reduce_visible_unfielded/token")
		b.ReportMetric(float64(s.parentReductionInvisibleFielded)/tokens, "parent_reduce_invisible_fielded/token")
		b.ReportMetric(float64(s.parentReductionInvisibleUnfielded)/tokens, "parent_reduce_invisible_unfielded/token")
		b.ReportMetric(float64(s.parentReductionVisibleChildPtrs)/tokens, "parent_reduce_visible_child_ptrs/token")
		b.ReportMetric(float64(s.parentReductionInvisibleChildPtrs)/tokens, "parent_reduce_invisible_child_ptrs/token")
		b.ReportMetric(float64(s.parentReductionVisibleLen0)/tokens, "parent_reduce_visible_len0/token")
		b.ReportMetric(float64(s.parentReductionVisibleLen1)/tokens, "parent_reduce_visible_len1/token")
		b.ReportMetric(float64(s.parentReductionVisibleLen2)/tokens, "parent_reduce_visible_len2/token")
		b.ReportMetric(float64(s.parentReductionVisibleLen3)/tokens, "parent_reduce_visible_len3/token")
		b.ReportMetric(float64(s.parentReductionVisibleLen4Plus)/tokens, "parent_reduce_visible_len4plus/token")
		b.ReportMetric(float64(s.parentReductionInvisibleLen0)/tokens, "parent_reduce_invisible_len0/token")
		b.ReportMetric(float64(s.parentReductionInvisibleLen1)/tokens, "parent_reduce_invisible_len1/token")
		b.ReportMetric(float64(s.parentReductionInvisibleLen2)/tokens, "parent_reduce_invisible_len2/token")
		b.ReportMetric(float64(s.parentReductionInvisibleLen3)/tokens, "parent_reduce_invisible_len3/token")
		b.ReportMetric(float64(s.parentReductionInvisibleLen4Plus)/tokens, "parent_reduce_invisible_len4plus/token")
		b.ReportMetric(float64(s.reduceChildSlicesFastGSS)/tokens, "reduce_child_slices_fast_gss/token")
		b.ReportMetric(float64(s.reduceChildPointersFastGSS)/tokens, "reduce_child_ptrs_fast_gss/token")
		b.ReportMetric(float64(s.reduceChildSlicesAllVisible)/tokens, "reduce_child_slices_all_visible/token")
		b.ReportMetric(float64(s.reduceChildPointersAllVisible)/tokens, "reduce_child_ptrs_all_visible/token")
		b.ReportMetric(float64(s.reduceChildSlicesScratchGeneral)/tokens, "reduce_child_slices_scratch_general/token")
		b.ReportMetric(float64(s.reduceChildPointersScratchGeneral)/tokens, "reduce_child_ptrs_scratch_general/token")
		b.ReportMetric(float64(s.reduceChildSlicesScratchNoAlias)/tokens, "reduce_child_slices_scratch_no_alias/token")
		b.ReportMetric(float64(s.reduceChildPointersScratchNoAlias)/tokens, "reduce_child_ptrs_scratch_no_alias/token")
		b.ReportMetric(float64(s.collapseRawUnaryAttempts)/tokens, "collapse_raw_attempts/token")
		b.ReportMetric(float64(s.collapseRawUnarySuccesses)/tokens, "collapse_raw_success/token")
		b.ReportMetric(float64(s.collapseRawUnaryMissShape)/tokens, "collapse_raw_miss_shape/token")
		b.ReportMetric(float64(s.collapseRawUnaryMissGrammar)/tokens, "collapse_raw_miss_grammar/token")
		b.ReportMetric(float64(s.collapseRawUnaryMissChild)/tokens, "collapse_raw_miss_child/token")
		b.ReportMetric(float64(s.collapseRawUnaryMissRule)/tokens, "collapse_raw_miss_rule/token")
		b.ReportMetric(float64(s.collapseUnaryAttempts)/tokens, "collapse_post_attempts/token")
		b.ReportMetric(float64(s.collapseUnarySuccesses)/tokens, "collapse_post_success/token")
		b.ReportMetric(float64(s.collapseUnaryMissShape)/tokens, "collapse_post_miss_shape/token")
		b.ReportMetric(float64(s.collapseUnaryMissGrammar)/tokens, "collapse_post_miss_grammar/token")
		b.ReportMetric(float64(s.collapseUnaryMissFielded)/tokens, "collapse_post_miss_fielded/token")
		b.ReportMetric(float64(s.collapseUnaryMissChild)/tokens, "collapse_post_miss_child/token")
		b.ReportMetric(float64(s.collapseUnaryMissRule)/tokens, "collapse_post_miss_rule/token")
		b.ReportMetric(float64(s.collapseRuleSameSymbol)/tokens, "collapse_rule_same_symbol/token")
		b.ReportMetric(float64(s.collapseRuleInvisibleWrapper)/tokens, "collapse_rule_invisible_wrapper/token")
		b.ReportMetric(float64(s.collapseRuleNamedLeafAlias)/tokens, "collapse_rule_named_leaf_alias/token")
		b.ReportMetric(float64(s.noTreePlaceholderNodesConstructed)/tokens, "notree_placeholder_nodes/token")
		b.ReportMetric(float64(s.otherNodesConstructed)/tokens, "other_nodes/token")
		b.ReportMetric(float64(s.extraNodesConstructed)/tokens, "extra_nodes/token")
		b.ReportMetric(float64(s.errorSymbolNodesConstructed)/tokens, "error_symbol_nodes/token")
		b.ReportMetric(float64(s.errorSymbolNodesConstructed)/tokens, "error_nodes/token")
		b.ReportMetric(float64(s.hasErrorNodesConstructed)/tokens, "has_error_nodes/token")
		b.ReportMetric(float64(s.arenaNodeStructBytesAllocated)/tokens, "arena_node_B/token")
		b.ReportMetric(float64(s.arenaNodeFieldMetadataBytesAllocated)/tokens, "arena_node_field_meta_B/token")
		b.ReportMetric(float64(s.arenaNoTreeNodeBytesAllocated)/tokens, "arena_notree_node_B/token")
		b.ReportMetric(float64(s.arenaCompactFullLeafBytesAllocated)/tokens, "arena_compact_full_leaf_B/token")
		b.ReportMetric(float64(s.arenaPendingParentBytesAllocated)/tokens, "arena_pending_parent_B/token")
		b.ReportMetric(float64(s.arenaPendingChildEntryBytesAllocated)/tokens, "arena_pending_child_entry_B/token")
		b.ReportMetric(float64(s.arenaFinalChildSidecarBytesAllocated)/tokens, "arena_final_child_sidecar_B/token")
		b.ReportMetric(float64(s.arenaFinalChildSidecarBytesAllocated)/tokens, "final_child_sidecar_B/token")
		b.ReportMetric(float64(s.arenaChildSliceBytesAllocated)/tokens, "arena_child_B/token")
		b.ReportMetric(float64(s.arenaChildSliceBytesAllocated)/tokens, "child_slice_B/token")
		b.ReportMetric(float64(s.arenaFieldIDBytesAllocated)/tokens, "arena_field_id_B/token")
		b.ReportMetric(float64(s.arenaFieldIDBytesAllocated)/tokens, "field_id_B/token")
		b.ReportMetric(float64(s.arenaFieldSourceBytesAllocated)/tokens, "arena_field_src_B/token")
		b.ReportMetric(float64(s.arenaFieldSourceBytesAllocated)/tokens, "field_source_B/token")
		b.ReportMetric(float64(s.mergeScratchBytesAllocated)/tokens, "merge_B/token")
		b.ReportMetric(float64(s.childSlicesConstructed)/tokens, "child_slices/token")
		b.ReportMetric(float64(s.childPointersConstructed)/tokens, "child_ptrs/token")
		b.ReportMetric(float64(s.childSlicesLen1)/tokens, "child_slices_len1/token")
		b.ReportMetric(float64(s.childSlicesLen2)/tokens, "child_slices_len2/token")
		b.ReportMetric(float64(s.childSlicesLen3)/tokens, "child_slices_len3/token")
		b.ReportMetric(float64(s.childSlicesLen4Plus)/tokens, "child_slices_len4plus/token")
		b.ReportMetric(float64(s.parentChildPointersConstructed)/tokens, "parent_child_ptrs/token")
		b.ReportMetric(float64(s.parentChildrenLen0)/tokens, "parent_len0/token")
		b.ReportMetric(float64(s.parentChildrenLen1)/tokens, "parent_len1/token")
		b.ReportMetric(float64(s.parentChildrenLen2)/tokens, "parent_len2/token")
		b.ReportMetric(float64(s.parentChildrenLen3)/tokens, "parent_len3/token")
		b.ReportMetric(float64(s.parentChildrenLen4Plus)/tokens, "parent_len4plus/token")
		b.ReportMetric(float64(s.fieldIDElementsConstructed)/tokens, "field_ids/token")
		b.ReportMetric(float64(s.fieldSourceElementsConstructed)/tokens, "field_sources/token")
	}
	b.ReportMetric(float64(s.externalCheckpointRecords)/tokens, "chk_records/token")
	b.ReportMetric(float64(s.externalCheckpointSlots)/tokens, "chk_slots/token")
	b.ReportMetric(float64(s.externalCheckpointBytes)/tokens, "chk_B/token")
	b.ReportMetric(float64(s.externalCheckpointSnapshotBytes)/tokens, "chk_snap_B/token")
	if s.externalCheckpointLeafNodes != 0 {
		b.ReportMetric(float64(s.externalCheckpointLeafNodes)/tokens, "checkpoint_leaf_full_nodes/token")
	}
	if s.compactFullLeafCreated != 0 {
		b.ReportMetric(float64(s.compactFullLeafCreated)/tokens, "compact_full_leaf_created/token")
		b.ReportMetric(float64(s.compactFullLeafMaterialized)/tokens, "compact_full_leaf_materialized/token")
		b.ReportMetric(float64(s.compactFullLeafMaterializedParent)/tokens, "compact_full_leaf_materialized_parent/token")
		reportPendingParentRejectStats(b, s.compactFullLeafParentRejects, tokens, "compact_full_leaf_materialized_parent_reject")
		reportPendingParentFieldRejectPayloadStats(b, s.compactFullLeafFieldRejectPayloads, tokens, "compact_full_leaf_materialized_parent_reject_fields_payload")
		b.ReportMetric(float64(s.compactFullLeafMaterializedFinal)/tokens, "compact_full_leaf_materialized_final/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.normalization)/tokens, "compact_full_leaf_materialized_normalization/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.recovery)/tokens, "compact_full_leaf_materialized_recovery/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.query)/tokens, "compact_full_leaf_materialized_query/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.cursor)/tokens, "compact_full_leaf_materialized_cursor/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.parentAPI)/tokens, "compact_full_leaf_materialized_parent_api/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.edit)/tokens, "compact_full_leaf_materialized_edit/token")
		b.ReportMetric(float64(s.compactFullLeafReasons.checkpointBuild)/tokens, "compact_full_leaf_materialized_checkpoint_rebuild/token")
		b.ReportMetric(float64(s.compactFullLeafDropped)/tokens, "compact_full_leaf_dropped/token")
		b.ReportMetric(float64(s.checkpointLeafFullNodesAvoided)/tokens, "checkpoint_leaf_full_nodes_avoided/token")
	}
	if s.pendingParentCreated != 0 {
		b.ReportMetric(float64(s.pendingParentCreated)/tokens, "pending_parent_created/token")
		b.ReportMetric(float64(s.pendingParentMaterialized)/tokens, "pending_parent_materialized/token")
		b.ReportMetric(float64(s.pendingParentMaterializedParent)/tokens, "pending_parent_materialized_parent/token")
		reportPendingParentRejectStats(b, s.pendingParentParentRejects, tokens, "pending_parent_materialized_parent_reject")
		reportPendingParentFieldRejectStats(b, s.pendingParentFieldRejects, tokens, "pending_parent_materialized_parent_reject_fields")
		reportPendingParentFieldRejectPayloadStats(b, s.pendingParentFieldRejectPayloads, tokens, "pending_parent_materialized_parent_reject_fields_payload")
		b.ReportMetric(float64(s.pendingParentMaterializedFinal)/tokens, "pending_parent_materialized_final/token")
		b.ReportMetric(float64(s.pendingParentReasons.normalization)/tokens, "pending_parent_materialized_normalization/token")
		b.ReportMetric(float64(s.pendingParentReasons.recovery)/tokens, "pending_parent_materialized_recovery/token")
		b.ReportMetric(float64(s.pendingParentReasons.query)/tokens, "pending_parent_materialized_query/token")
		b.ReportMetric(float64(s.pendingParentReasons.cursor)/tokens, "pending_parent_materialized_cursor/token")
		b.ReportMetric(float64(s.pendingParentReasons.parentAPI)/tokens, "pending_parent_materialized_parent_api/token")
		b.ReportMetric(float64(s.pendingParentReasons.edit)/tokens, "pending_parent_materialized_edit/token")
		b.ReportMetric(float64(s.pendingParentReasons.checkpointBuild)/tokens, "pending_parent_materialized_checkpoint_rebuild/token")
		b.ReportMetric(float64(s.pendingParentDropped)/tokens, "pending_parent_dropped/token")
		b.ReportMetric(float64(s.pendingParentsFlattened)/tokens, "pending_parent_flattened/token")
		b.ReportMetric(float64(s.pendingChildRefsFlattened)/tokens, "pending_child_refs_flattened/token")
		b.ReportMetric(float64(s.pendingChildEntriesAllocated)/tokens, "pending_child_entries_allocated/token")
		b.ReportMetric(float64(s.pendingChildEntryCapacity)/tokens, "pending_child_entry_capacity/token")
		b.ReportMetric(float64(s.pendingChildEntryWaste)/tokens, "pending_child_entry_waste/token")
		b.ReportMetric(float64(s.pendingParentCandidates)/tokens, "pending_parent_candidate/token")
		if s.preMatFieldRejectCandidates != 0 {
			b.ReportMetric(float64(s.preMatFieldRejectCandidates)/tokens, "pre_materialization_field_reject_candidate/token")
			b.ReportMetric(float64(s.preMatFieldRejectSameKey)/tokens, "pre_materialization_field_reject_same_key/token")
			b.ReportMetric(float64(s.preMatFieldRejectOverflow)/tokens, "pre_materialization_field_reject_overflow/token")
		}
		b.ReportMetric(float64(s.pendingParentRejects.empty)/tokens, "pending_parent_reject_empty/token")
		b.ReportMetric(float64(s.pendingParentRejects.childLimit)/tokens, "pending_parent_reject_child_limit/token")
		b.ReportMetric(float64(s.pendingParentRejects.alias)/tokens, "pending_parent_reject_alias/token")
		b.ReportMetric(float64(s.pendingParentRejects.rawSpan)/tokens, "pending_parent_reject_raw_span/token")
		b.ReportMetric(float64(s.pendingParentRejects.fields)/tokens, "pending_parent_reject_fields/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsParentHide)/tokens, "pending_parent_reject_fields_parent_hidden/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsNoIDs)/tokens, "pending_parent_reject_fields_no_ids/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsInherited)/tokens, "pending_parent_reject_fields_inherited/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHidden)/tokens, "pending_parent_reject_fields_hidden_child/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHiddenPlain)/tokens, "pending_parent_reject_fields_hidden_child_plain/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHiddenPlainEmpty)/tokens, "pending_parent_reject_fields_hidden_child_plain_empty/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHiddenPlainOne)/tokens, "pending_parent_reject_fields_hidden_child_plain_one/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHiddenPlainMany)/tokens, "pending_parent_reject_fields_hidden_child_plain_many/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsHiddenWithFields)/tokens, "pending_parent_reject_fields_hidden_child_with_fields/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsChild)/tokens, "pending_parent_reject_fields_child/token")
		b.ReportMetric(float64(s.pendingParentRejects.fieldsAllVisible)/tokens, "pending_parent_reject_fields_all_visible_direct/token")
		b.ReportMetric(float64(s.pendingParentRejects.child)/tokens, "pending_parent_reject_child/token")
		b.ReportMetric(float64(s.pendingParentRejects.span)/tokens, "pending_parent_reject_span/token")
		b.ReportMetric(float64(s.pendingParentRejects.fill)/tokens, "pending_parent_reject_fill/token")
	}
	if s.resultSelectionNanos != 0 || s.transientParentMaterializeNanos != 0 || s.resultTreeBuildNanos != 0 || s.transientChildMaterializeNanos != 0 {
		ops := float64(s.ops)
		b.ReportMetric(float64(s.resultSelectionNanos)/ops, "result_select_ns/op")
		b.ReportMetric(float64(s.transientParentMaterializeNanos)/ops, "transient_parent_materialize_ns/op")
		b.ReportMetric(float64(s.resultTreeBuildNanos)/ops, "result_tree_build_ns/op")
		b.ReportMetric(float64(s.transientChildMaterializeNanos)/ops, "transient_child_materialize_ns/op")
		b.ReportMetric(float64(s.resultPythonKeywordRepairNanos)/ops, "result_python_keyword_repair_ns/op")
		b.ReportMetric(float64(s.resultPythonRootRepairNanos)/ops, "result_python_root_repair_ns/op")
		b.ReportMetric(float64(s.resultFinalizeRootNanos)/ops, "result_finalize_root_ns/op")
		b.ReportMetric(float64(s.resultExtendTrailingNanos)/ops, "result_extend_trailing_ns/op")
		b.ReportMetric(float64(s.resultNormalizeRootStartNanos)/ops, "result_normalize_root_start_ns/op")
		b.ReportMetric(float64(s.resultCompatibilityNanos)/ops, "result_compatibility_ns/op")
		b.ReportMetric(float64(s.resultParentLinkNanos)/ops, "result_parent_link_ns/op")
	}
	b.ReportMetric(float64(s.normalizationNodesVisited)/tokens, "norm_visited/token")
	b.ReportMetric(float64(s.normalizationNodesRewritten)/tokens, "norm_rewritten/token")
}

func loadPythonCorpusFile(tb testing.TB) pythonCorpusFile {
	tb.Helper()

	candidates := []string{strings.TrimSpace(os.Getenv("GOT_PYTHON_CORPUS_FILE"))}
	if candidates[0] == "" {
		candidates = []string{
			defaultPythonCorpusPath,
			filepath.Join("cgo_harness", defaultPythonCorpusPath),
		}
	}

	for _, path := range candidates {
		if path == "" {
			continue
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() {
			continue
		}
		source, err := os.ReadFile(path)
		if err != nil {
			tb.Fatalf("read python corpus %s: %v", path, err)
		}
		if len(source) == 0 {
			tb.Fatalf("python corpus %s is empty", path)
		}
		tb.Logf("python corpus: path=%s bytes=%d", path, len(source))
		return pythonCorpusFile{path: path, source: source}
	}

	tb.Fatalf("python corpus file not found; tried %s; set GOT_PYTHON_CORPUS_FILE or run from the repository/cgo_harness root", strings.Join(candidates, ", "))
	return pythonCorpusFile{}
}

func pythonCorpusBenchTimeoutMicros(tb testing.TB) uint64 {
	tb.Helper()

	raw := strings.TrimSpace(os.Getenv("GOT_PYTHON_CORPUS_BENCH_TIMEOUT_MICROS"))
	if raw == "" {
		return 0
	}
	timeoutMicros, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		tb.Fatalf("invalid GOT_PYTHON_CORPUS_BENCH_TIMEOUT_MICROS=%q", raw)
	}
	return timeoutMicros
}

func pythonCorpusArenaBreakdownEnabled(tb testing.TB) bool {
	tb.Helper()

	raw := strings.TrimSpace(os.Getenv("GOT_PYTHON_CORPUS_ARENA_BREAKDOWN"))
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		tb.Fatalf("invalid GOT_PYTHON_CORPUS_ARENA_BREAKDOWN=%q", raw)
	}
	return enabled
}

func pythonCorpusRuntimeAuditEnabled(tb testing.TB) bool {
	tb.Helper()

	raw := strings.TrimSpace(os.Getenv("GOT_PYTHON_CORPUS_RUNTIME_AUDIT"))
	if raw == "" {
		return false
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		tb.Fatalf("invalid GOT_PYTHON_CORPUS_RUNTIME_AUDIT=%q", raw)
	}
	return enabled
}

func requireCompletePythonCorpusTree(tb testing.TB, lang *gotreesitter.Language, file pythonCorpusFile, tree *gotreesitter.Tree, phase string) {
	tb.Helper()

	if tree == nil {
		tb.Fatalf("%s: parse returned nil tree", phase)
	}
	root := tree.RootNode()
	if root == nil {
		tb.Fatalf("%s: parse returned nil root", phase)
	}
	rt := tree.ParseRuntime()
	if root.HasError() || tree.ParseStoppedEarly() || root.EndByte() != uint32(len(file.source)) || rt.Truncated {
		tb.Fatalf(
			"%s: incomplete parse path=%s type=%q has_error=%v stopped=%v root_end=%d want=%d runtime=%s",
			phase,
			file.path,
			root.Type(lang),
			root.HasError(),
			tree.ParseStoppedEarly(),
			root.EndByte(),
			len(file.source),
			rt.Summary(),
		)
	}
}

func benchmarkPythonCorpusGoDFA(b *testing.B, mode pythonCorpusParseMode) {
	file := loadPythonCorpusFile(b)
	lang := grammars.PythonLanguage()
	if pythonCorpusArenaBreakdownEnabled(b) {
		gotreesitter.EnableArenaBreakdown(true)
		defer gotreesitter.EnableArenaBreakdown(false)
	}
	if pythonCorpusRuntimeAuditEnabled(b) {
		gotreesitter.EnableRuntimeAudit(true)
		defer gotreesitter.EnableRuntimeAudit(false)
	}
	pool := gotreesitter.NewParserPool(
		lang,
		gotreesitter.WithParserPoolTimeoutMicros(pythonCorpusBenchTimeoutMicros(b)),
	)

	b.ReportAllocs()
	b.SetBytes(int64(len(file.source)))
	b.ResetTimer()

	var stats pythonRuntimeBenchStats
	for i := 0; i < b.N; i++ {
		var (
			tree *gotreesitter.Tree
			err  error
		)
		switch mode {
		case pythonCorpusParseModeDFA:
			tree, err = pool.Parse(file.source)
		case pythonCorpusParseModeDFANoCompat:
			tree, err = pool.ParseNoResultCompatibilityBenchmarkOnly(file.source)
		case pythonCorpusParseModeDFANoTree:
			tree, err = pool.ParseNoTreeBenchmarkOnly(file.source)
		case pythonCorpusParseModeDFANoTreeWithCheckpoints:
			tree, err = pool.ParseNoTreeWithExternalCheckpointsBenchmarkOnly(file.source)
		default:
			b.Fatalf("unknown python corpus parse mode %q", mode)
		}
		if err != nil {
			if tree != nil {
				tree.Release()
			}
			b.Fatalf("%s: %v", file.path, err)
		}
		requireCompletePythonCorpusTree(b, lang, file, tree, string(mode))
		rt := tree.ParseRuntime()
		breakdown, hasBreakdown := tree.ArenaBreakdown()
		stats.add(rt, breakdown, hasBreakdown)
		tree.Release()
	}
	b.StopTimer()
	stats.report(b)
}

func BenchmarkPythonCorpusGoTreeSitterParseDFA(b *testing.B) {
	benchmarkPythonCorpusGoDFA(b, pythonCorpusParseModeDFA)
}

func BenchmarkPythonCorpusGoTreeSitterParseDFANoCompat(b *testing.B) {
	benchmarkPythonCorpusGoDFA(b, pythonCorpusParseModeDFANoCompat)
}

func BenchmarkPythonCorpusGoTreeSitterParseDFANoTree(b *testing.B) {
	benchmarkPythonCorpusGoDFA(b, pythonCorpusParseModeDFANoTree)
}

func BenchmarkPythonCorpusGoTreeSitterParseDFANoTreeWithCheckpoints(b *testing.B) {
	benchmarkPythonCorpusGoDFA(b, pythonCorpusParseModeDFANoTreeWithCheckpoints)
}

func BenchmarkPythonCorpusGoTreeSitterParseDFAWithImportExtract(b *testing.B) {
	file := loadPythonCorpusFile(b)
	lang := grammars.PythonLanguage()
	pool := gotreesitter.NewParserPool(
		lang,
		gotreesitter.WithParserPoolTimeoutMicros(pythonCorpusBenchTimeoutMicros(b)),
	)

	b.ReportAllocs()
	b.SetBytes(int64(len(file.source)))
	b.ResetTimer()

	var imports int64
	for i := 0; i < b.N; i++ {
		tree, err := pool.Parse(file.source)
		if err != nil {
			if tree != nil {
				tree.Release()
			}
			b.Fatalf("%s: %v", file.path, err)
		}
		requireCompletePythonCorpusTree(b, lang, file, tree, "dfa_import_extract")
		imports += int64(len(gotreesitter.ExtractImports(tree)))
		tree.Release()
	}
	if b.N > 0 {
		b.ReportMetric(float64(imports)/float64(b.N), "imports/op")
	}
}

func BenchmarkPythonCorpusSourceImportExtract(b *testing.B) {
	file := loadPythonCorpusFile(b)
	lang := grammars.PythonLanguage()

	b.ReportAllocs()
	b.SetBytes(int64(len(file.source)))
	b.ResetTimer()

	var imports int64
	var fallbacks int64
	for i := 0; i < b.N; i++ {
		report := gotreesitter.ExtractImportsFromSourceWithReport(lang, file.source)
		imports += int64(len(report.Imports))
		if report.FallbackRecommended {
			fallbacks++
		}
	}
	if b.N > 0 {
		b.ReportMetric(float64(imports)/float64(b.N), "imports/op")
		b.ReportMetric(float64(fallbacks)/float64(b.N), "fallbacks/op")
	}
}

func BenchmarkPythonCorpusCTreeSitterParseFull(b *testing.B) {
	file := loadPythonCorpusFile(b)
	parser := newCTreeSitterParserWithLanguage(b, sitterpython.GetLanguage)
	defer parser.Close()

	b.ReportAllocs()
	b.SetBytes(int64(len(file.source)))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		tree := parseCTree(b, parser, nil, file.source, file.path)
		root := requireCompleteCTree(b, tree, file.source, file.path)
		if root.HasError() {
			tree.Close()
			b.Fatalf("%s: cgo parse has errors", file.path)
		}
		tree.Close()
	}
}
