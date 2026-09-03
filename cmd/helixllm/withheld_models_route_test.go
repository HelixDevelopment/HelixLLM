package main

// A model this deployment is NOT serving must be published as withheld, with
// the reason — and must be impossible to mistake for one that is served.
//
// # The condition
//
// api.Model.WithheldReason is declared and documented at length, and until this
// change nothing in non-test code ever wrote it: modelsFromOptions dropped every
// unavailable option (`if !opt.Available { continue }`) and stamped the survivors
// `Availability: "serving"`. Downstream, helix_agent DECODES withheld_reason and
// VALIDATES it against a closed set — so the consumer was fully prepared for a
// field the producer never sent, and its withheld branch could not fire.
//
// That silence is a data-loss root cause, not a cosmetic gap. A host whose
// backend is LOADING and a host that has WITHDRAWN a model both publish the same
// thing — an absence — so a consuming tool sweeping for models it can no longer
// find cannot tell "wait, it is coming back" from "gone, drop it", and deletes a
// restarting host's configuration.
//
// # The load-bearing risk this change carries
//
// modelsFromOptions' own comment states why it dropped them: "a model shown as
// usable that is not being served costs the caller a failed request." Publishing
// them must not undo that. So the checks below are not only "is it listed" —
// they drive the actual predicates the two known consumers apply, including the
// Claude Toolkit's jq filter verbatim, and confirm a withheld entry survives
// NONE of them as usable.
//
// # Polarity (§11.4.115)
//
//	RED_MODE=1 go test -run TestListModels_Withheld ./cmd/helixllm/
//	           go test -run TestListModels_Withheld ./cmd/helixllm/
//
// RED_MODE=1 asserts the pre-fix behaviour (the withheld model is absent), so a
// run against the unfixed tree PASSES and proves the reproduction is real.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/HelixDevelopment/HelixLLM/pkg/api"
)

// listModels drives the real GET /v1/models route and returns both the decoded
// listing and the raw body, because one consumer's predicate is a jq program
// that has to run against the actual bytes.
func (s *servingStack) listModels(t *testing.T) (api.ModelList, string) {
	t.Helper()
	w := httptest.NewRecorder()
	s.engine.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("GET /v1/models returned %d: %s", w.Code, w.Body.String())
	}
	var list api.ModelList
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode listing: %v\nbody: %s", err, w.Body.String())
	}
	return list, w.Body.String()
}

// withheldStack is one down local backend and one serving cloud provider, which
// is the ordinary state of a workstation whose llama-server is still loading.
func withheldStack(t *testing.T) (*servingStack, *recordingProvider, *recordingProvider) {
	t.Helper()
	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b"},
		down:   true,
	}
	cloud := &recordingProvider{name: "chutes", models: []string{"deepseek-chat"}}
	return newServingStack(t, local, cloud), local, cloud
}

// find returns the listing entry whose identity ends in the served name.
func find(list api.ModelList, servedName string) (api.Model, bool) {
	for _, m := range list.Data {
		if m.ModelIdentity != "" && strings.HasSuffix(m.ModelIdentity, "/"+servedName) {
			return m, true
		}
	}
	return api.Model{}, false
}

// TestListModels_WithheldModelIsPublishedWithItsReason pins the producer side.
func TestListModels_WithheldModelIsPublishedWithItsReason(t *testing.T) {
	stack, _, _ := withheldStack(t)
	list, body := stack.listModels(t)

	got, present := find(list, "llama3:8b")

	if redMode() {
		if present {
			t.Fatalf("RED_MODE=1 expected the withheld model to be ABSENT from the pre-fix "+
				"listing, but it is present: %+v", got)
		}
		return
	}

	if !present {
		t.Fatalf("the withheld model is not in the listing at all: %s\n"+
			"A consuming tool cannot tell a host whose backend is LOADING from one that has "+
			"WITHDRAWN the model when both publish the same absence — and one of those two "+
			"answers is 'delete the user's configuration'.", body)
	}
	if got.Availability != "withheld" {
		t.Errorf("availability = %q, want %q: %+v\n"+
			"A consumer admits only the recorded states; anything else degrades to "+
			"'not reported', which loses the distinction this entry exists to draw.",
			got.Availability, "withheld", got)
	}
	if got.WithheldReason == "" {
		t.Errorf("withheld_reason is empty on a withheld entry: %+v\n"+
			"The reason is the only part of the answer a user can act on.", got)
	}
	// The provider is simply not up, and that is the reason the wire must carry —
	// the one a consumer can distinguish from a permanent withdrawal.
	if got.WithheldReason != "provider_unavailable" {
		t.Errorf("withheld_reason = %q, want %q: %+v",
			got.WithheldReason, "provider_unavailable", got)
	}

	// The served option must be untouched by this change.
	served, ok := find(list, "deepseek-chat")
	if ok {
		t.Errorf("the remote vendor's model acquired an identity (%q); it must not",
			served.ModelIdentity)
	}
	var cloudEntry api.Model
	for _, m := range list.Data {
		if m.ID == "deepseek-chat" {
			cloudEntry = m
		}
	}
	if cloudEntry.ID == "" {
		t.Fatalf("the serving cloud model vanished from the listing: %s", body)
	}
	if cloudEntry.Availability != "serving" {
		t.Errorf("a SERVING model reports availability %q, want %q",
			cloudEntry.Availability, "serving")
	}
	if cloudEntry.WithheldReason != "" {
		t.Errorf("a serving model carries withheld_reason %q; a reason belongs to a "+
			"withholding and to nothing else", cloudEntry.WithheldReason)
	}
}

// TestListModels_WithheldEntryIsNotConsumableAsUsable is the load-bearing guard.
//
// Publishing withheld options re-opens exactly the hazard modelsFromOptions'
// comment names, so this drives the real predicates of every known consumer of
// this listing rather than asserting a field in isolation.
func TestListModels_WithheldEntryIsNotConsumableAsUsable(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: RED_MODE reproduces the pre-fix artifact, where no withheld " +
			"entry is published and there is therefore nothing for a consumer to mistake")
	}

	stack, local, cloud := withheldStack(t)
	list, body := stack.listModels(t)

	withheld, present := find(list, "llama3:8b")
	if !present {
		t.Fatalf("no withheld entry to test: %s", body)
	}

	// THE PRODUCER-SIDE INVARIANT, asserted first because it is the only half of
	// this we can guarantee.
	//
	// Every consumer filter we know of treats an ABSENT availability as serving
	// — reasonably, since before withheld options reached the wire the listing
	// itself was the affirmative act. So a withheld entry that merely OMITS the
	// field is selected by all of them. What makes it excludable is that we mark
	// it EXPLICITLY, and that is a property this server controls.
	//
	// Assert it here rather than relying on the snapshot below, because a copy
	// of someone else's filter can go stale without anything noticing, while
	// this cannot.
	t.Run("the withheld entry carries an explicit availability, not an absent one", func(t *testing.T) {
		if withheld.Availability == "" {
			t.Fatal("the withheld entry omits availability. Every consumer filter we know of " +
				"defaults an absent value to serving, so an omitted marker is not a weaker " +
				"signal — it is the opposite signal, and the entry would be consumed as usable.")
		}
		if withheld.Availability == "serving" {
			t.Fatalf("the withheld entry is marked availability=%q", withheld.Availability)
		}
	})

	// CONSUMER 1 — the Claude Toolkit. Its _CMA_HELIXLLM_SERVING_JQ is the ONE
	// definition of "this host is serving us this model right now", used both to
	// build its provider records and to decide whether a host proved it is
	// serving. Run by real jq against the real body: asserting on a paraphrase
	// would prove nothing about the tool that ships.
	//
	// HONEST LIMIT OF THIS CHECK. The string below is a SNAPSHOT of
	// scripts/claude-providers.sh's `_CMA_HELIXLLM_SERVING_JQ`, taken by hand.
	// It is not read from that script, because reaching into a sibling checkout
	// would hardcode another project's path here and would SKIP on any host
	// without it — and a guard that disappears when its subject is absent is the
	// same class of problem this whole change is about.
	//
	// So: if the toolkit changes its filter, this test keeps passing against the
	// OLD one and cannot tell you. It proves the body we emit is excluded by the
	// filter AS OF this copy; it does not prove the shipped toolkit excludes it
	// today. The toolkit's own suite now derives the filter from its script at
	// run time and carries a withheld fixture, so that side is covered where it
	// can be. This side is a cross-check, not the guarantee.
	const toolkitServingJQSnapshot = `select((.model_identity // "") != "")
  | select((.availability // "serving") == "serving")`
	t.Run("claude toolkit serving filter excludes it", func(t *testing.T) {
		jq, err := exec.LookPath("jq")
		if err != nil {
			t.Skip("SKIP-OK: jq is not installed on this host, so the toolkit's own " +
				"filter cannot be run against the real body here; the Go-side " +
				"equivalence check below still runs")
		}
		cmd := exec.Command(jq, "-c", ".data[] | "+toolkitServingJQSnapshot)
		cmd.Stdin = strings.NewReader(body)
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("running the toolkit's filter: %v", err)
		}
		selected := string(out)
		if strings.Contains(selected, withheld.ID) {
			t.Errorf("the toolkit's serving filter SELECTED the withheld model %q:\n%s\n"+
				"It would be written into a user's provider configuration as a target, "+
				"and every request routed to it costs a failed call.", withheld.ID, selected)
		}
		// The filter must still select the models that ARE served, or this test
		// would pass on a listing that published nothing at all.
		if !strings.Contains(selected, "deepseek-chat") && len(list.Data) > 1 {
			// deepseek-chat is a remote vendor model and carries no identity, so
			// the toolkit's first clause excludes it by design. Assert instead
			// that the filter is not vacuously empty for a served LOCAL model.
			t.Log("no locally-served model in this fixture; the vacuity check is " +
				"covered by TestListModels_ServingLocalModelSurvivesTheToolkitFilter")
		}
	})

	// CONSUMER 2 — helix_agent's catalog. It admits ONLY the recorded states:
	// availabilityFromWire maps anything that is not exactly "serving" or
	// "withheld" to AvailabilityUnreported, and Availability.Usable() is true
	// for "serving" alone. So the single fact that keeps a withheld entry out of
	// its selectable set is that our value is never "serving".
	t.Run("helix_agent treats it as not usable", func(t *testing.T) {
		if withheld.Availability == "serving" {
			t.Fatalf("the withheld entry reports availability %q; helix_agent binds "+
				"Entry.Enabled to Availability.Usable(), which is true for exactly "+
				"that value", withheld.Availability)
		}
		if withheld.Availability != "withheld" {
			t.Errorf("availability = %q — not 'serving' (so not usable) but also not the "+
				"recorded 'withheld', so helix_agent degrades it to 'not reported' and "+
				"the REASON is discarded with it", withheld.Availability)
		}
	})

	// CONSUMER 3 — this gateway itself. A listing entry is only meaningful if a
	// request naming it behaves consistently, so the strongest check is that
	// asking for the withheld identifier does not get served.
	t.Run("a request naming it is not served", func(t *testing.T) {
		w := stack.chat(t, withheld.ID)
		if w.Code == http.StatusOK {
			t.Errorf("POST /v1/chat/completions with the withheld identifier %q returned 200: %s\n"+
				"Publishing it as withheld must not make it reachable.", withheld.ID, w.Body.String())
		}
		if got := local.received(); len(got) != 0 {
			t.Errorf("the down provider %q was dispatched to with %v", local.name, got)
		}
		if got := cloud.received(); len(got) != 0 {
			t.Errorf("the %q provider answered %v for a withheld model; a named model that "+
				"is not served must fail, never be substituted", cloud.name, got)
		}
	})
}

// TestListModels_ServingLocalModelSurvivesTheToolkitFilter is the vacuity guard
// for the filter check above: a filter that selects NOTHING would "exclude the
// withheld entry" trivially, and would also break every working deployment.
func TestListModels_ServingLocalModelSurvivesTheToolkitFilter(t *testing.T) {
	if redMode() {
		t.Skip("SKIP-OK: RED_MODE reproduces the pre-fix artifact; this guard pins " +
			"post-fix behaviour that must not regress")
	}

	local := &recordingProvider{
		name:   "llamacpp",
		host:   "gpu-01",
		models: []string{"llama3:8b", "qwen2.5:7b"},
	}
	stack := newServingStack(t, local)
	_, body := stack.listModels(t)

	jq, err := exec.LookPath("jq")
	if err != nil {
		t.Skip("SKIP-OK: jq is not installed on this host")
	}
	const toolkitServingJQ = `select((.model_identity // "") != "")
  | select((.availability // "serving") == "serving")`
	cmd := exec.Command(jq, "-r", ".data[] | "+toolkitServingJQ+" | .id")
	cmd.Stdin = strings.NewReader(body)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running the toolkit's filter: %v", err)
	}
	if strings.TrimSpace(string(out)) == "" {
		t.Fatalf("the toolkit's serving filter selected NOTHING from a listing of two "+
			"served local models: %s\nA filter that excludes everything would pass the "+
			"withheld-entry check for the wrong reason and would leave every real "+
			"deployment with no providers.", body)
	}
}
