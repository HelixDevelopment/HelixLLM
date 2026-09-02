// Package capability measures what a host can currently support.
//
// Single responsibility: produce a HostCapabilityProfile from the live machine —
// CPU, available system memory, accelerators (bound by stable device identity,
// never an enumeration index), and free storage as an axis independent of memory.
//
// It answers "what is here?" and nothing else. It never decides which model to
// run: that is [selection]'s job, and keeping the two apart is what makes
// selection a pure function that can be tested without hardware.
package capability
