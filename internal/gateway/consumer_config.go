package gateway

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/HelixDevelopment/HelixLLM/internal/brain"
	"github.com/HelixDevelopment/HelixLLM/internal/naming"
	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// Consumer-configuration endpoints.
//
// WHY THESE EXIST.
//
// `internal/naming` has been able to produce HelixCode and OpenCode
// configuration since the naming scheme landed, and both exporters are covered
// by unit tests — but nothing outside that package ever called them. The only
// instructions a user had were Go snippets in docs/guides/consumer_setup.md
// invoking an `internal/` package, which is not importable from outside this
// module. So the artefacts existed and no user could obtain one: the Claude
// Toolkit read its models off `GET /v1/models`, and the other two consumers had
// no path at all.
//
// These handlers close that gap by following the Claude Toolkit's own
// precedent — the thing a user already points their tool at is this gateway's
// HTTP API, so that is where the configuration is served from too, under the
// same /v1 group, the same auth, and the same rate limit.
//
// WHY THE ENDPOINT IS THE BASE URL WE HAND OUT.
//
// Both exports point a consumer at an OpenAI-compatible endpoint, and the
// endpoint MUST be this gateway rather than the llama.cpp instance behind it:
// the identifiers we publish are OURS, and only this gateway maps one back to
// the model name a provider answers to (brain.Brain.ResolveModelName). Handing
// out the backend's own address would publish identifiers nothing there can
// resolve. The address used is the one the request arrived on, so an operator
// behind a reverse proxy gets the public name their proxy presented rather than
// a value this process would have to be told separately.
//
// NEITHER ENDPOINT WRITES TO A USER'S FILES (FR-018). The GET returns the
// artefact; the POST takes the user's current file content in the request body
// and returns the merged content. Both hand the result back — what lands on
// disk stays the user's decision.

// consumerHelixCode and consumerOpenCode are the path values naming each
// supported consumer. They are lower-case single words so the route reads as
// `/v1/config/helixcode`.
const (
	consumerHelixCode = "helixcode"
	consumerOpenCode  = "opencode"
)

// mergeBodyLimit caps the configuration file a caller may submit for merging.
// A user's env file or opencode.json is kilobytes; this bounds the read so a
// hostile or mistaken caller cannot make the gateway buffer an arbitrary
// amount of memory.
const mergeBodyLimit = 1 << 20 // 1 MiB

// exportedModel is one option in the response, flattened so a consumer reading
// the JSON never has to parse the identity to recover a field.
type exportedModel struct {
	Identifier string `json:"identifier"`
	Identity   string `json:"identity"`
	WireModel  string `json:"wire_model"`
}

// withheldModel is one option deliberately not exported, with its reason.
type withheldModel struct {
	Identity string `json:"identity"`
	Reason   string `json:"reason"`
}

// consumerConfigResponse is the envelope both consumers answer with. Each
// consumer fills the artefact field its own tooling reads and leaves the
// other empty.
type consumerConfigResponse struct {
	Consumer string `json:"consumer"`
	Host     string `json:"host"`
	BaseURL  string `json:"base_url"`

	// EnvFile is HelixCode's environment-file fragment.
	EnvFile string `json:"env_file,omitempty"`

	// ProviderID and Document are OpenCode's provider key and configuration
	// fragment. Document is embedded as real JSON rather than a string so a
	// caller can merge it with jq without unquoting first.
	ProviderID string          `json:"provider_id,omitempty"`
	Document   json.RawMessage `json:"document,omitempty"`

	Models   []exportedModel `json:"models"`
	Withheld []withheldModel `json:"withheld"`
}

// HandleConsumerConfig handles GET /v1/config/:consumer.
func HandleConsumerConfig(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		consumer := strings.ToLower(strings.TrimSpace(c.Param("consumer")))
		if consumer != consumerHelixCode && consumer != consumerOpenCode {
			configError(c, http.StatusNotFound, i18n.KeyGatewayConfigUnknownConsumer,
				map[string]string{"consumer": consumer})
			return
		}

		inst, ok := instanceForRequest(c, b)
		if !ok {
			return
		}

		switch consumer {
		case consumerHelixCode:
			cfg, err := naming.ExportHelixCode(inst)
			if err != nil {
				configError(c, http.StatusInternalServerError, i18n.KeyGatewayConfigExportFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			if refusedAsUndescribable(c, inst.Host, cfg.Models, cfg.Withheld) {
				return
			}
			c.JSON(http.StatusOK, consumerConfigResponse{
				Consumer: consumer,
				Host:     inst.Host,
				BaseURL:  inst.BaseURL,
				EnvFile:  cfg.EnvFile,
				Models:   toExported(cfg.Models),
				Withheld: toWithheld(cfg.Withheld),
			})
		case consumerOpenCode:
			cfg, err := naming.ExportOpenCode(inst)
			if err != nil {
				configError(c, http.StatusInternalServerError, i18n.KeyGatewayConfigExportFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			if refusedAsUndescribable(c, inst.Host, cfg.Models, cfg.Withheld) {
				return
			}
			c.JSON(http.StatusOK, consumerConfigResponse{
				Consumer:   consumer,
				Host:       inst.Host,
				BaseURL:    inst.BaseURL,
				ProviderID: cfg.ProviderID,
				Document:   json.RawMessage(cfg.Document),
				Models:     toExported(cfg.Models),
				Withheld:   toWithheld(cfg.Withheld),
			})
		}
	}
}

// HandleConsumerConfigMerge handles POST /v1/config/:consumer/merge.
//
// The request body is the caller's CURRENT configuration file; the response is
// that same file with this instance's managed section added or replaced and
// everything else preserved. Nothing is written here — the caller decides
// whether the returned content lands on disk (FR-018).
func HandleConsumerConfigMerge(b *brain.Brain) gin.HandlerFunc {
	return func(c *gin.Context) {
		consumer := strings.ToLower(strings.TrimSpace(c.Param("consumer")))
		if consumer != consumerHelixCode && consumer != consumerOpenCode {
			configError(c, http.StatusNotFound, i18n.KeyGatewayConfigUnknownConsumer,
				map[string]string{"consumer": consumer})
			return
		}

		existing, err := io.ReadAll(io.LimitReader(c.Request.Body, mergeBodyLimit))
		if err != nil {
			configError(c, http.StatusBadRequest, i18n.KeyGatewayConfigBodyUnreadable,
				map[string]string{"detail": err.Error()})
			return
		}

		inst, ok := instanceForRequest(c, b)
		if !ok {
			return
		}

		switch consumer {
		case consumerHelixCode:
			cfg, err := naming.ExportHelixCode(inst)
			if err != nil {
				configError(c, http.StatusInternalServerError, i18n.KeyGatewayConfigExportFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			if refusedAsUndescribable(c, inst.Host, cfg.Models, cfg.Withheld) {
				return
			}
			merged, err := naming.MergeHelixCodeEnv(string(existing), cfg)
			if err != nil {
				// The caller's file and ours disagree in a way neither can win
				// silently — report it rather than pick for them.
				configError(c, http.StatusConflict, i18n.KeyGatewayConfigMergeFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(merged))
		case consumerOpenCode:
			cfg, err := naming.ExportOpenCode(inst)
			if err != nil {
				configError(c, http.StatusInternalServerError, i18n.KeyGatewayConfigExportFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			if refusedAsUndescribable(c, inst.Host, cfg.Models, cfg.Withheld) {
				return
			}
			merged, err := naming.MergeOpenCode(existing, cfg)
			if err != nil {
				configError(c, http.StatusConflict, i18n.KeyGatewayConfigMergeFailed,
					map[string]string{"detail": err.Error()})
				return
			}
			c.Data(http.StatusOK, "application/json; charset=utf-8", merged)
		}
	}
}

// instanceForRequest builds the serving instance the export describes, or
// answers the request with the reason it cannot and reports false.
//
// One instance, not several. Each export describes exactly one serving host —
// HelixCode's live route reads a single endpoint variable, and OpenCode's
// `options.baseURL` is per provider entry — so when this gateway fronts more
// than one host the caller has to say which. Choosing for them would silently
// hand out one host's models under a configuration that mentions no host.
func instanceForRequest(c *gin.Context, b *brain.Brain) (naming.Instance, bool) {
	if b == nil {
		configError(c, http.StatusNotFound, i18n.KeyGatewayConfigNoServedHosts, nil)
		return naming.Instance{}, false
	}

	// Group the options by the host serving them. A remote vendor's model
	// carries no host and no identity by design (FR-014), so it is not part of
	// any instance's offers.
	byHost := map[string][]naming.Offer{}
	healthy := map[string]bool{}
	unhealthyReason := map[string]string{}

	for _, opt := range b.ModelOptions() {
		host := strings.ToLower(strings.TrimSpace(opt.Host))
		if host == "" || opt.Identity == "" {
			continue
		}
		id, err := naming.ParseIdentity(opt.Identity)
		if err != nil {
			// An identity we published but cannot read back is a defect in the
			// listing, not something to paper over by exporting a guess.
			continue
		}
		byHost[host] = append(byHost[host], naming.Offer{
			Identity:  id,
			Available: opt.Available,
			Reason:    opt.Reason,
		})
		if opt.Available {
			healthy[host] = true
		} else if _, seen := unhealthyReason[host]; !seen && opt.Reason != "" {
			unhealthyReason[host] = opt.Reason
		}
	}

	if len(byHost) == 0 {
		configError(c, http.StatusNotFound, i18n.KeyGatewayConfigNoServedHosts, nil)
		return naming.Instance{}, false
	}

	hosts := make([]string, 0, len(byHost))
	for h := range byHost {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)

	host := strings.ToLower(strings.TrimSpace(c.Query("host")))
	switch {
	case host == "" && len(hosts) == 1:
		host = hosts[0]
	case host == "":
		configError(c, http.StatusBadRequest, i18n.KeyGatewayConfigHostAmbiguous,
			map[string]string{"hosts": strings.Join(hosts, ", ")})
		return naming.Instance{}, false
	}
	if _, ok := byHost[host]; !ok {
		configError(c, http.StatusNotFound, i18n.KeyGatewayConfigHostUnknown,
			map[string]string{"host": host})
		return naming.Instance{}, false
	}

	inst := naming.Instance{
		Host:    host,
		BaseURL: requestBaseURL(c),
		Healthy: healthy[host],
		Reason:  unhealthyReason[host],
		Offers:  byHost[host],
	}
	return inst, true
}

// requestBaseURL is the origin the caller reached this gateway on, with no API
// version path — each export appends whatever its own client requires.
//
// The scheme is upgraded to https when a terminating proxy says the client's
// leg was encrypted, and never downgraded on that header's say-so: reporting
// http for a connection the user made over https would hand them a
// configuration that silently drops the encryption they were using.
func requestBaseURL(c *gin.Context) string {
	scheme := "http"
	if c.Request != nil && c.Request.TLS != nil {
		scheme = "https"
	}
	if strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	host := ""
	if c.Request != nil {
		host = c.Request.Host
	}
	return fmt.Sprintf("%s://%s", scheme, host)
}

// refusedAsUndescribable answers the request with the reason a configuration
// cannot be produced yet, and reports whether it did.
//
// THE DEFECT THIS CLOSES. A backend that is up but LOADING reports itself
// unavailable while still listing its models, so the export withheld every one
// of them and both endpoints answered 200 — the GET with a document whose
// `models` was `{}`, the merge with the caller's file rewritten to hold that
// empty entry. A user who fetched their configuration during a restart got
// their working provider replaced by one offering nothing, and the returned
// document said nothing about why. Whether they lost their models came down to
// when they happened to ask.
//
// The reasons were already known — the response carried them in a sibling
// `withheld` field the merge never consulted — so this is not new information,
// only information that now reaches the decision. An empty roster with reasons
// behind it is the export declining to describe the instance, not a description
// of an instance with no models.
//
// WHY REFUSING CANNOT STRAND A GENUINELY EMPTY SERVER. An instance reaches
// here only through instanceForRequest, which builds one solely for a host that
// contributed at least one option; a host serving nothing at all is already
// answered by KeyGatewayConfigNoServedHosts before any export runs. So an
// instance with no offers cannot arrive, an instance with offers always
// partitions into at least one exported or one withheld option, and the state
// this refuses is exactly "has offers, can serve none" — never "has nothing".
//
// 503 rather than 200-with-nothing, and rather than 404: the host exists and is
// expected back, which is what this codebase already answers 503 for when an
// identifier belongs to a host whose list has not filled yet. No Retry-After
// accompanies it — this process is not told how long a backend takes to load,
// and a number invented here would be a guess presented as a schedule.
func refusedAsUndescribable(c *gin.Context, host string, models []naming.Exported, withheld []naming.WithheldOption) bool {
	if len(models) > 0 || len(withheld) == 0 {
		return false
	}
	configError(c, http.StatusServiceUnavailable, i18n.KeyGatewayConfigNothingServable,
		map[string]string{
			"host":    host,
			"count":   strconv.Itoa(len(withheld)),
			"reasons": naming.WithheldReasons(withheld),
		})
	return true
}

func toExported(in []naming.Exported) []exportedModel {
	// Explicitly empty, never nil: `"models": []` states "none", while
	// `"models": null` reads as a malformed body.
	out := make([]exportedModel, 0, len(in))
	for _, m := range in {
		out = append(out, exportedModel{Identifier: m.Identifier, Identity: m.Identity, WireModel: m.WireModel})
	}
	return out
}

func toWithheld(in []naming.WithheldOption) []withheldModel {
	out := make([]withheldModel, 0, len(in))
	for _, w := range in {
		out = append(out, withheldModel{Identity: w.Identity, Reason: w.Reason})
	}
	return out
}

// configError answers with an OpenAI-compatible error body carrying a message
// resolved through the request's Accept-Language (CONST-046).
func configError(c *gin.Context, status int, key string, vars map[string]string) {
	var msg string
	if vars == nil {
		msg = tr(c, key)
	} else {
		msg = tr(c, key, vars)
	}
	c.JSON(status, api.ErrorResponse{
		Error: api.ErrorDetail{Message: msg, Type: "invalid_request_error"},
	})
}
