package enforce

const (
	CodeRequiredTestMissing        = "REQUIRED_TEST_MISSING"
	CodeAnnotationUnknownDirective = "ANNOTATION_UNKNOWN_DIRECTIVE"
	CodeAnnotationMissingValue     = "ANNOTATION_MISSING_VALUE"
	CodeAnnotationBadStateTrans    = "ANNOTATION_BAD_STATE_TRANSITION"
	CodeAnnotationUnknownInvariant = "ANNOTATION_UNKNOWN_INVARIANT"
	CodeHashSchemaNoProducer       = "HASH_SCHEMA_NO_PRODUCER"
	CodeHashSchemaNoConsumer       = "HASH_SCHEMA_NO_CONSUMER"
	CodeGraphSourceFileMissing     = "GRAPH_SOURCE_FILE_MISSING"
	CodeStaleSourceFileNode        = CodeGraphSourceFileMissing
	CodeInvariantNoEnforcer        = "INVARIANT_NO_ENFORCER"
	CodePackageContractMissing     = "PACKAGE_CONTRACT_MISSING"
	CodeDependencyCycleDangerous   = "DEPENDENCY_CYCLE_DANGEROUS"

	// Backward-compatible aliases used by existing triage/action mappings.
	CodeMalformedStateTransition = CodeAnnotationBadStateTrans
	CodeAnnotationBadIdentifier  = "ANNOTATION_BAD_IDENTIFIER"
	CodeAnnotationBadTestName    = "ANNOTATION_BAD_TEST_NAME"
	CodeAnnotationRefInvariantMissing = CodeAnnotationUnknownInvariant
	CodeAnnotationRefTestMissing = "ANNOTATION_REF_TEST_MISSING"
	CodeRequiredTestNoPath       = "REQUIRED_TEST_NO_PATH"
	CodeMissingHashProducer      = CodeHashSchemaNoProducer
	CodeMissingHashConsumer      = CodeHashSchemaNoConsumer
	CodeHashSchemaOrphaned       = "HASH_SCHEMA_ORPHANED"
	CodeOrphanedHashSchema       = CodeHashSchemaOrphaned
	CodeOrphanedInvariantNode    = CodeInvariantNoEnforcer
	CodeNoGraph                  = "NO_GRAPH"

	// Scaffold test quality
	CodeScaffoldTodoSkip        = "SCAFFOLD_TODO_SKIP"
	CodeDoneFixcaseScaffoldOnly = "DONE_FIXCASE_SCAFFOLD_ONLY"

	// Graph Go-file coverage
	CodeGraphCoverageLow      = "GRAPH_COVERAGE_LOW"
	CodeGraphCoverageCritical = "GRAPH_COVERAGE_CRITICAL"

	// Invariant implementation coverage ratchet
	CodeInvariantCoverageBelowThreshold = "INVARIANT_COVERAGE_BELOW_THRESHOLD"

	// Invariant shape integrity codes.
	CodeInvariantNoImplementation    = "INVARIANT_NO_IMPLEMENTATION"
	CodeInvariantNoTestCoverage      = "INVARIANT_NO_TEST_COVERAGE"
	CodeInvariantNoFailureMode       = "INVARIANT_NO_FAILURE_MODE"
	CodeInvariantNoForbiddenFix      = "INVARIANT_NO_FORBIDDEN_FIX"
	CodeInvariantOrphanImpl          = "INVARIANT_ORPHAN_IMPLEMENTATION"
	CodeInvariantMissingAuthority    = "INVARIANT_MISSING_AUTHORITY"
	CodeInvariantUnverifiedImpl      = "INVARIANT_UNVERIFIED_IMPLEMENTATION"
	CodeInvariantGuardsUnreachable   = "INVARIANT_GUARDS_ACTION_UNREACHABLE"
	CodeInvariantViolatedNoTest      = "INVARIANT_VIOLATED_NO_TEST"
	CodeInvariantForbiddenFixNoGuard = "INVARIANT_FORBIDDEN_FIX_NO_GUARD"
)
