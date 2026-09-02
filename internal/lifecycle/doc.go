// Package lifecycle governs how a running model gives memory back.
//
// Single responsibility: unload models idle beyond a configurable period, refuse
// to evict a model that is currently serving a request, and explain every
// self-initiated unload.
//
// A model must never leave the available set without the user being told which
// one went and why — a silent disappearance is indistinguishable from a fault.
package lifecycle
