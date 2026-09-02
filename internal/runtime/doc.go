// Package runtime decides which execution path serves a chosen model, and
// launches it.
//
// Single responsibility: prefer the general in-memory path; fall through to the
// disk-streaming path only when the model does not fit in memory AND is a member
// of that runtime's declared supported roster AND meets its own minimums.
//
// Eligibility for streaming is a roster lookup, never an inference from the
// model's architecture — several architecturally-suitable models have no support
// path, and offering one would produce an option that cannot run.
package runtime
