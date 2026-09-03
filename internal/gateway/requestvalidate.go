package gateway

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// Semantic request validation for the completion endpoints.
//
// ShouldBindJSON catches JSON *syntax* errors and nothing else, so a body
// that parses but means nothing — no messages, a negative token budget, a
// model name that is not a model name — used to travel all the way to
// provider dispatch and come back as whatever the backend said. The caller
// who sent the bad request was told the SERVER had failed.
//
// This file is the missing step between binding and dispatch. It is shared
// by /v1/chat/completions, /v1/completions, and /v1/messages so the three
// endpoints cannot drift into three different answers for the same mistake.
//
// # What is validated, and on whose authority
//
// Nothing here is invented. Each rule cites either the vendor API this
// gateway is compatible with, or this project's own challenge banks, which
// are the executable specification wherever they speak:
//
//	messages non-empty     OpenAI OpenAPI CreateChatCompletionRequest
//	                       .messages carries `minItems: 1`; bank
//	                       challenges/banks/regression/known_bugs.yaml
//	                       CH-REG-001 asserts 400. Anthropic's Messages API
//	                       likewise takes "a single user-role message, or ...
//	                       multiple".
//	role non-empty         bank CH-REG-006 (`"role": null` -> 400). OpenAI's
//	                       request-message union discriminates on `role`, so
//	                       a message with no role matches no variant.
//	max_tokens floor       PER ENDPOINT, deliberately:
//	                         chat  >= 1  banks CH-REG-007 (-1 -> 400) and
//	                                     CH-REG-004 (0 -> 400). OpenAI's
//	                                     chat schema declares no minimum
//	                                     (the field is `deprecated` there),
//	                                     so the banks govern.
//	                         legacy >= 0 OpenAI CreateCompletionRequest
//	                                     .max_tokens `minimum: 0` — zero is
//	                                     legal, it scores a prompt with
//	                                     logprobs/echo without generating.
//	                         messages >= 0 Anthropic documents max_tokens 0
//	                                     as prompt-cache pre-warming.
//	                       The asymmetry is the vendors', not ours; flatten
//	                       it and one of the three endpoints starts
//	                       rejecting a documented, legitimate request.
//	model well-formed      length, charset, and no path-traversal segment.
//	                       Banks CH-REG-010 (330-character name -> 400),
//	                       CH-REG-002 (non-ASCII -> 400), and
//	                       challenges/banks/security/owasp.yaml CH-SEC-007
//	                       (`../../etc/passwd` -> 400).
//
// # What is deliberately NOT validated
//
// Rejecting something a real client legitimately sends would be a worse
// regression than the wrong status this fixes, so the following are left
// alone on purpose:
//
//   - Message CONTENT is never required to be non-empty. An assistant turn
//     carrying tool_calls has empty content by design.
//   - The role ENUM is not enforced, only non-emptiness. OpenAI has
//     `developer` and `function` beyond this project's four roles and adds
//     more over time; rejecting an unknown-but-present role would reject
//     requests a correct client sends to a newer API.
//   - `n`, `temperature`, `top_p`, `presence_penalty`, `frequency_penalty`:
//     OpenAI declares ranges, but no bank speaks and no defect was reported.
//   - Model EXISTENCE. A well-formed but unrecognised model still reaches
//     dispatch. "Requested resource does not exist" is 404 in OpenAI's
//     error-codes guide and already belongs to /v1/models/:id. This file
//     rejects names that are not model NAMES, never names that are merely
//     unknown.
//   - An empty `model`, which both handlers already default. Changing that
//     would break every client relying on the default.
//   - AUTHENTICATION. An empty or malformed bearer token is a 401 answered
//     by middleware.APIKeyAuth. A malformed request and an unauthenticated
//     one are different answers with different status codes, and this file
//     never reads the Authorization header so it cannot collapse them.

const (
	// modelNameMaxLen bounds a model identifier. The longest identifier
	// this project's own configuration uses is 44 characters
	// ("meta-llama/Llama-3.3-70B-Instruct-Turbo-Free"); bank CH-REG-010
	// requires a 330-character name to be rejected. 128 sits well clear of
	// every real identifier — including a fully qualified
	// registry/org/name:tag — and well below the case that must fail.
	//
	// Measured in BYTES. The charset below admits only ASCII, so for any
	// name that could pass, bytes and characters are the same count; a
	// multi-byte name is rejected either way, only with a different reason
	// depending on which check trips first.
	modelNameMaxLen = 128

	// modelNameExtraChars are the non-alphanumeric characters that appear
	// in real model identifiers across the providers this gateway fronts:
	//
	//	.  version and registry segments   ghcr.io, llama.cpp, gpt-3.5
	//	_  vendor naming                   q4_K_M
	//	-  ubiquitous                      llama-3.1-70b
	//	/  org / repo pairs                meta-llama/Llama-3.3-70B
	//	:  Ollama tags, Bedrock versions   llama3:8b, ...-v1:0
	//	@  digest pinning                  llama3@sha256:...
	//	+  build metadata
	//
	// Anything outside this set is not a model name, which is what makes
	// bank CH-REG-002's non-ASCII case a malformed request rather than an
	// unknown model.
	modelNameExtraChars = "._-/:@+"
)

// requestDefect is one semantic fault in a request: the message to render,
// the OpenAI `error.param` naming the offending field, and the OpenAI
// `error.code` classifying it.
//
// It carries a param because a client cannot fix a field the error does not
// name — the single most useful thing a 400 can say.
type requestDefect struct {
	msgKey string
	args   map[string]string
	param  string
	code   string
}

// OpenAI error codes reused verbatim so a client library that switches on
// `error.code` sees the vocabulary it already knows.
const (
	codeEmptyArray          = "empty_array"
	codeInvalidValue        = "invalid_value"
	codeStringAboveMaxLen   = "string_above_max_length"
	codeMissingRequiredParm = "missing_required_parameter"
)

// write renders the defect as an OpenAI-shaped 400.
func (d *requestDefect) write(c *gin.Context) {
	param, code := d.param, d.code
	c.JSON(http.StatusBadRequest, api.ErrorResponse{
		Error: api.ErrorDetail{
			Message: tr(c, d.msgKey, d.args),
			Type:    "invalid_request_error",
			Param:   &param,
			Code:    &code,
		},
	})
}

// validateModelName checks that model is a syntactically plausible model
// identifier. An empty model is NOT a defect: both handlers substitute a
// default, and that behaviour predates this file.
func validateModelName(model string) *requestDefect {
	if model == "" {
		return nil
	}
	if len(model) > modelNameMaxLen {
		return &requestDefect{
			msgKey: i18n.KeyGatewayModelTooLong,
			args: map[string]string{
				"max":    strconv.Itoa(modelNameMaxLen),
				"length": strconv.Itoa(len(model)),
			},
			param: "model",
			code:  codeStringAboveMaxLen,
		}
	}
	for _, r := range model {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune(modelNameExtraChars, r):
		default:
			return &requestDefect{
				msgKey: i18n.KeyGatewayModelInvalidChars,
				args:   map[string]string{"allowed": modelNameExtraChars},
				param:  "model",
				code:   codeInvalidValue,
			}
		}
	}
	// Path traversal. Checked per segment rather than by substring so that
	// a legitimate org/repo identifier keeps its slashes and only a "." or
	// ".." SEGMENT is refused.
	for _, seg := range strings.Split(model, "/") {
		if seg == "." || seg == ".." {
			return &requestDefect{
				msgKey: i18n.KeyGatewayModelPathTraversal,
				param:  "model",
				code:   codeInvalidValue,
			}
		}
	}
	return nil
}

// validateMaxTokens applies the per-endpoint floor. min is 1 for
// /v1/chat/completions and 0 for the legacy and Anthropic endpoints; see
// the file header for why the three differ.
func validateMaxTokens(maxTokens *int, min int) *requestDefect {
	if maxTokens == nil || *maxTokens >= min {
		return nil
	}
	return &requestDefect{
		msgKey: i18n.KeyGatewayMaxTokensTooSmall,
		args: map[string]string{
			"min":   strconv.Itoa(min),
			"value": strconv.Itoa(*maxTokens),
		},
		param: "max_tokens",
		code:  codeInvalidValue,
	}
}

// validateMessageRoles checks that every message declares a role. roles is
// indexed the same as the wire array so the reported param points at the
// exact offending element.
func validateMessageRoles(roles []string) *requestDefect {
	for i, role := range roles {
		if strings.TrimSpace(role) != "" {
			continue
		}
		return &requestDefect{
			msgKey: i18n.KeyGatewayMessageRoleMissing,
			args:   map[string]string{"index": strconv.Itoa(i)},
			param:  "messages[" + strconv.Itoa(i) + "].role",
			code:   codeMissingRequiredParm,
		}
	}
	return nil
}

// emptyMessagesDefect is the shared "messages must not be empty" fault. An
// absent `messages` key and an explicit `[]` are the same mistake to the
// caller and get the same answer.
func emptyMessagesDefect() *requestDefect {
	return &requestDefect{
		msgKey: i18n.KeyGatewayMessagesEmpty,
		param:  "messages",
		code:   codeEmptyArray,
	}
}

// validateChatRequest applies the /v1/chat/completions policy.
func validateChatRequest(req *api.ChatCompletionRequest) *requestDefect {
	if d := validateModelName(req.Model); d != nil {
		return d
	}
	if len(req.Messages) == 0 {
		return emptyMessagesDefect()
	}
	roles := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		roles[i] = m.Role
	}
	if d := validateMessageRoles(roles); d != nil {
		return d
	}
	// chat: max_tokens >= 1 (banks CH-REG-004 / CH-REG-007).
	return validateMaxTokens(req.MaxTokens, 1)
}

// validateCompletionRequest applies the legacy /v1/completions policy. It
// has no messages array, and its max_tokens floor is 0 per OpenAI's own
// schema.
func validateCompletionRequest(req *api.CompletionRequest) *requestDefect {
	if d := validateModelName(req.Model); d != nil {
		return d
	}
	return validateMaxTokens(req.MaxTokens, 0)
}

// validateMessageRequest applies the Anthropic /v1/messages policy. Its
// max_tokens floor is 0 because Anthropic documents 0 as prompt-cache
// pre-warming.
func validateMessageRequest(req *api.MessageRequest) *requestDefect {
	if d := validateModelName(req.Model); d != nil {
		return d
	}
	if len(req.Messages) == 0 {
		return emptyMessagesDefect()
	}
	roles := make([]string, len(req.Messages))
	for i, m := range req.Messages {
		roles[i] = m.Role
	}
	if d := validateMessageRoles(roles); d != nil {
		return d
	}
	// MaxTokens is a non-pointer int on MessageRequest, so an omitted field
	// is indistinguishable from 0 — and 0 is legal here, so only an
	// explicitly negative value can be a defect.
	return validateMaxTokens(&req.MaxTokens, 0)
}
