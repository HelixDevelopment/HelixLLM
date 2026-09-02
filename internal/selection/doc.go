// Package selection joins a measured host against the catalogue to produce
// the options that host can actually serve.
//
// Single responsibility: given (HostCapabilityProfile, catalogue, declared
// usage), return offered options and, for everything withheld, exactly one
// reason — insufficient resources, unsupported configuration, or excluded by
// usage terms. Those have different remedies and must never collapse into one
// generic unavailability.
//
// Offers come back ordered, cheapest-admissible-first: memory required, then
// storage required, then the catalogue identity. Preference is NOT left to the
// caller. A host serves several models at once — a coder model beside a vision
// or video one, sharing one accelerator — so the cheapest option that genuinely
// runs is the one that leaves room for the next. The ordering is a tie-break
// among options that already passed configuration, fit and terms; it never
// promotes something that was withheld. It mirrors select() in
// container/helix_model_gate.py so both paths choose alike.
//
// The join is one-directional: selection reads measurement and never writes
// back into it. That property is deliberate — it is what allows the entire
// surface, refusals included, to be exercised from fixture hosts.
package selection
