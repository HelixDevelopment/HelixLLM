package main

import (
	"strings"
	"testing"
)

// TestDescribeAuthPosture pins the four credential states and, more
// importantly, that the OPEN one is loud.
//
// The defect this whole change answers was an operator unable to tell
// protected from unprotected. Silence was the failure mode, so "the open case
// is reported as Open, and carries advice naming both variables" is a
// behaviour worth a test rather than a comment.
func TestDescribeAuthPosture(t *testing.T) {
	cases := []struct {
		name       string
		apiKeys    string
		jwtEnabled bool
		wantOpen   bool
		wantIn     []string
	}{
		{
			name: "neither configured", wantOpen: true,
			wantIn: []string{"NONE"},
		},
		{
			name: "api keys only", apiKeys: "sk-live",
			wantIn: []string{"API key"},
		},
		{
			name: "jwt only", jwtEnabled: true,
			wantIn: []string{"JWT"},
		},
		{
			name: "both", apiKeys: "sk-live", jwtEnabled: true,
			wantIn: []string{"API key", "JWT"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := describeAuthPosture(tc.apiKeys, tc.jwtEnabled)

			if p.Open != tc.wantOpen {
				t.Errorf("Open = %v, want %v", p.Open, tc.wantOpen)
			}
			if p.Summary == "" {
				t.Error("Summary is empty; the startup log would say nothing")
			}
			for _, want := range tc.wantIn {
				if !strings.Contains(p.Summary, want) {
					t.Errorf("Summary %q does not mention %q", p.Summary, want)
				}
			}

			// Never leak the key list into the operator-facing summary.
			if tc.apiKeys != "" && strings.Contains(p.Summary+p.Advice, tc.apiKeys) {
				t.Errorf("the posture text contains the configured API key value")
			}
		})
	}
}

// TestOpenPostureAdviceNamesBothVariables: the whole point of the WARN is that
// an operator reading it knows what to do next.
func TestOpenPostureAdviceNamesBothVariables(t *testing.T) {
	p := describeAuthPosture("", false)

	if p.Advice == "" {
		t.Fatal("the open posture carries no advice; a warning that does not say what to set is noise")
	}
	for _, v := range []string{"HELIX_AUTH_API_KEYS", "HELIX_AUTH_JWT_SECRET"} {
		if !strings.Contains(p.Advice, v) {
			t.Errorf("advice does not name %s: %q", v, p.Advice)
		}
	}
}

// TestJWTOnlyPostureWarnsAboutTheBootstrap: with no API keys, POST
// /v1/auth/token has nothing to exchange and answers 401 forever. An operator
// who does not know that reads it as a broken server, so the startup log says
// it.
func TestJWTOnlyPostureWarnsAboutTheBootstrap(t *testing.T) {
	p := describeAuthPosture("", true)

	if p.Open {
		t.Error("a configured JWT secret must not report an open posture")
	}
	if !strings.Contains(p.Advice, "/v1/auth/token") {
		t.Errorf("advice does not warn that the exchange endpoint has no credential "+
			"to exchange: %q", p.Advice)
	}
}
