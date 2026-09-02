// Package failover handles a serving host lost while a request is in flight.
//
// Single responsibility: detect the loss, optionally retry on an equivalent
// model elsewhere, and tell the user whenever a retry occurred.
//
// One invariant dominates: no single answer may ever be composed from more than
// one model instance. A retry replaces an answer; it never splices one.
package failover
