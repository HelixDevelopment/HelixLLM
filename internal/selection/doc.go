// Package selection joins a measured host against the catalogue to produce
// the options that host can actually serve.
//
// Single responsibility: given (HostCapabilityProfile, catalogue, declared
// usage), return offered options and, for everything withheld, exactly one
// reason — insufficient resources, unsupported configuration, or excluded by
// usage terms. Those have different remedies and must never collapse into one
// generic unavailability.
//
// The join is one-directional: selection reads measurement and never writes
// back into it. That property is deliberate — it is what allows the entire
// surface, refusals included, to be exercised from fixture hosts.
package selection
