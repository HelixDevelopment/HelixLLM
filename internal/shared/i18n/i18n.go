// Package i18n wraps digital.vasic.i18n to provide multi-language API
// error messages for HelixLLM.  English messages for the most common
// API errors are pre-loaded at construction time.
package i18n

import (
	upstream "digital.vasic.i18n/pkg/i18n"
)

// English error message keys.
const (
	KeyInvalidAPIKey       = "invalid_api_key"
	KeyRateLimitExceeded   = "rate_limit_exceeded"
	KeyModelNotFound       = "model_not_found"
	KeyInvalidRequest      = "invalid_request"
	KeyInternalError       = "internal_error"

	// CONST-046 round-95 — CLI user-facing strings migrated from
	// hardcoded literals in cmd/helixllm/{challenges,main}.go.
	KeyHelixllmCLIFailedToLoadBanks  = "helixllm_cli_failed_to_load_banks"
	KeyHelixllmCLIErrorLoadingConfig = "helixllm_cli_error_loading_config"

	// CONST-046 round-321 — cluster-monitor TUI user-facing strings
	// migrated from hardcoded literals in internal/control/tui.go.
	KeyMonitorTitle        = "monitor_title"
	KeyMonitorTitleRule    = "monitor_title_rule"
	KeyMonitorNoHosts      = "monitor_no_hosts"
	KeyMonitorLastCheck    = "monitor_last_check"
	KeyMonitorColHost      = "monitor_col_host"
	KeyMonitorColStatus    = "monitor_col_status"
	KeyMonitorColCPUCores  = "monitor_col_cpu_cores"
	KeyMonitorColMemoryMB  = "monitor_col_memory_mb"
	KeyMonitorColDeploys   = "monitor_col_deployments"
	KeyMonitorClusterState = "monitor_cluster_state"
	KeyMonitorOverallOK    = "monitor_overall_healthy"
	KeyMonitorOverallBad   = "monitor_overall_degraded"

	// CONST-046 round-391 — HTTP gateway + control-plane API
	// user-facing strings migrated from hardcoded literals in
	// internal/gateway/{openai,anthropic,websocket}.go and
	// internal/control/api.go.
	KeyGatewayInvalidRequestBody  = "gateway_invalid_request_body"
	KeyGatewayInvalidRequest      = "gateway_invalid_request"
	KeyGatewayBrainError          = "gateway_brain_error"
	KeyGatewayBrainStreamError    = "gateway_brain_stream_error"
	KeyGatewayModelNotFound       = "gateway_model_not_found"
	KeyGatewayGreeting            = "gateway_greeting"
	KeyGatewayHelpAcknowledgement = "gateway_help_acknowledgement"
	KeyControlNoServicesSpecified = "control_no_services_specified"
	KeyControlSchedulingFailed    = "control_scheduling_failed"
	KeyControlRebalanceFailed     = "control_rebalance_scheduling_failed"

	// CONST-046 round-410 — CLI challenge-runner output, CLI startup
	// errors, cluster-monitor remediation reasons, deployer host-status
	// messages, and knowledge-API request errors migrated from hardcoded
	// literals in cmd/helixllm/{challenges,main}.go,
	// internal/control/{monitor,deployer}.go, and internal/knowledge/api.go.
	KeyHelixllmCLIChallengeFail        = "helixllm_cli_challenge_fail"
	KeyHelixllmCLIChallengeSummary     = "helixllm_cli_challenge_summary"
	KeyHelixllmCLIInvalidConfig        = "helixllm_cli_invalid_config"
	KeyHelixllmCLIGenericError         = "helixllm_cli_generic_error"
	KeyControlRemediationAlertNoHosts  = "control_remediation_alert_no_healthy_hosts"
	KeyControlRemediationReschedule    = "control_remediation_reschedule"
	KeyControlRemediationRestartFailed = "control_remediation_restart_failed"
	KeyControlRemediationRestartOK     = "control_remediation_restart_ok"
	KeyControlHostUnreachable          = "control_host_unreachable"
	KeyKnowledgeInvalidRequestBody     = "knowledge_invalid_request_body"

	// CONST-036 / FR-019 — the no-backend model-listing reasons. A server
	// with no Brain configured serves NO models; these keys state that fact
	// so an empty listing is legible rather than looking like a broken
	// server. They replace a fabricated three-entry list that advertised
	// models this server could never serve.
	KeyGatewayNoBackendModels        = "gateway_no_backend_models"
	KeyGatewayNoBackendModelNotFound = "gateway_no_backend_model_not_found"
)

// defaultEnglishMessages is pre-loaded into every new Translator.
var defaultEnglishMessages = map[string]string{
	KeyInvalidAPIKey:     "Invalid API key provided",
	KeyRateLimitExceeded: "Rate limit exceeded, please try again later",
	KeyModelNotFound:     "The model '{{model}}' does not exist",
	KeyInvalidRequest:    "Invalid request: {{detail}}",
	KeyInternalError:     "Internal server error",

	KeyHelixllmCLIFailedToLoadBanks:  "failed to load banks: {{detail}}",
	KeyHelixllmCLIErrorLoadingConfig: "error loading config: {{detail}}",

	KeyMonitorTitle:        "HelixLLM Cluster Monitor",
	KeyMonitorTitleRule:    "========================",
	KeyMonitorNoHosts:      "No hosts configured.",
	KeyMonitorLastCheck:    "Last check: {{time}}",
	KeyMonitorColHost:      "HOST",
	KeyMonitorColStatus:    "STATUS",
	KeyMonitorColCPUCores:  "CPU CORES",
	KeyMonitorColMemoryMB:  "MEMORY (MB)",
	KeyMonitorColDeploys:   "DEPLOYMENTS",
	KeyMonitorClusterState: "Cluster: {{overall}}  |  Hosts: {{hosts}}  |  Last check: {{time}}",
	KeyMonitorOverallOK:    "healthy",
	KeyMonitorOverallBad:   "DEGRADED",

	KeyGatewayInvalidRequestBody:  "invalid request body: {{detail}}",
	KeyGatewayInvalidRequest:      "invalid request: {{detail}}",
	KeyGatewayBrainError:          "brain error: {{detail}}",
	KeyGatewayBrainStreamError:    "brain stream error: {{detail}}",
	KeyGatewayModelNotFound:       "model {{model}} not found",
	KeyGatewayGreeting:            "Hello! I'm HelixLLM.",
	KeyGatewayHelpAcknowledgement: "Yes, I can help with that. What would you like me to do?",
	KeyControlNoServicesSpecified: "no services specified",
	KeyControlSchedulingFailed:    "scheduling failed: {{detail}}",
	KeyControlRebalanceFailed:     "rebalance scheduling failed: {{detail}}",

	KeyHelixllmCLIChallengeFail:    "FAIL: {{id}} - {{error}}",
	KeyHelixllmCLIChallengeSummary: "{{passed}} passed, {{failed}} failed, {{skipped}} skipped",
	KeyHelixllmCLIInvalidConfig:    "invalid config: {{detail}}",
	KeyHelixllmCLIGenericError:     "error: {{detail}}",
	KeyControlRemediationAlertNoHosts: "service {{service}} failed {{attempts}} times and no healthy hosts " +
		"are available for rescheduling",
	KeyControlRemediationReschedule: "service {{service}} failed {{attempts}} consecutive restarts " +
		"on {{host}}; rescheduling to {{target}}",
	KeyControlRemediationRestartFailed: "restart attempt {{attempt}} failed: {{detail}}",
	KeyControlRemediationRestartOK:     "restarted {{service}} on {{host}} (attempt {{attempt}})",
	KeyControlHostUnreachable:          "host {{host}} is unreachable",
	KeyKnowledgeInvalidRequestBody:     "invalid request body: {{detail}}",

	KeyGatewayNoBackendModels: "no model-serving backend is configured on this server, " +
		"so no models are being served",
	KeyGatewayNoBackendModelNotFound: "model {{model}} not found: no model-serving backend " +
		"is configured on this server, so no models are being served",
}

// TranslatorAPI is the minimal contract that call sites depend on so
// they can be unit-tested with a fake implementation without binding
// to the upstream Bundle. The concrete *Translator below satisfies it.
//
// Decoupling per CONST-051(B): this interface lives in HelixLLM's own
// i18n package and references no consumer-project type — HelixLLM is
// reusable as a standalone repository.
type TranslatorAPI interface {
	T(lang, key string, vars ...map[string]string) string
}

// Translator wraps an upstream Bundle and exposes a simplified API
// that accepts string variable maps instead of interface{} maps.
type Translator struct {
	bundle *upstream.Bundle
}

// New creates a Translator whose default (fallback) language is
// defaultLang.  English messages for the common API error keys are
// pre-loaded automatically.
func New(defaultLang string) *Translator {
	b := upstream.NewBundle(defaultLang)
	b.AddMessages("en", defaultEnglishMessages)
	return &Translator{bundle: b}
}

// LoadMessages adds (or merges) messages for the given language tag.
// Call this before the first call to T for that language.
func (t *Translator) LoadMessages(lang string, messages map[string]string) {
	t.bundle.AddMessages(lang, messages)
}

// T returns the localised message for key in language lang.
// vars is an optional replacement map: keys match {{placeholder}} tokens
// in the message template.  Variable substitution is delegated to the
// upstream Bundle.
// Falls back to the default language when the key is absent in lang.
// Returns key itself when the key is absent in all loaded languages.
func (t *Translator) T(lang, key string, vars ...map[string]string) string {
	var params map[string]interface{}
	if len(vars) > 0 && vars[0] != nil {
		params = make(map[string]interface{}, len(vars[0]))
		for k, v := range vars[0] {
			params[k] = v
		}
	}
	if params != nil {
		return t.bundle.GetMessage(lang, key, params)
	}
	return t.bundle.GetMessage(lang, key)
}
