// Package telemetry observes models while they serve.
//
// Single responsibility: track per-model memory and accelerator use, record
// per-request latency and throughput, and expose both to users and in a form an
// automated check can consume.
//
// It also supplies the freshness that resource refusals depend on: a refusal
// must derive from a current reading, never one taken at start-up.
package telemetry
