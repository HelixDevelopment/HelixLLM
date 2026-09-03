package runtime

import (
	"github.com/HelixDevelopment/HelixLLM/internal/selection"
	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

// SelectionReserve is the reserve to select under on a host whose accelerator
// this process will later admit against — that is, on every host these binaries
// run on.
//
// # Why this is stated here and not discovered there
//
// Selection and the admission gate answer two halves of one question, and until
// this value existed they answered them on different terms. Selection asked
// "does the model fit the card AS MEASURED" and held nothing back; the broker
// keeps [vrambroker.HeadroomBytes] free above every reservation so an admission
// never drives the card to its edge. The band between those two answers is
// real: a 10 GiB model on an 11.5 GiB card clears selection's question and
// fails the broker's. The user was shown it, chose it, and it did not start —
// which is worse than never having been offered it, because the failure lands
// after the decision rather than instead of it.
//
// The margin is the BROKER'S policy. It is a property of how that gate admits,
// not something selection could derive from a measurement, so selection must be
// TOLD it. [selection.Reserve.AcceleratorHeadroomBytes] is the seam for saying
// so, and it defaults to zero precisely so that a caller with no admission gate
// behind it is not silently charged for one.
//
// So the statement lives HERE, in the package that already joins the two —
// [Acquire] takes a [selection.Option] and hands its recorded requirement to
// the broker — and not inside selection, which stays a pure function of
// (host, catalogue, declared usage) with no runtime component in its imports.
// Each boot binary is a composition root, and there are several; putting the
// statement in one of them would leave the others disagreeing with the gate,
// so they state it by calling this rather than by each repeating it.
//
// The value is read from [vrambroker.HeadroomBytes] rather than copied, so the
// two cannot hold different numbers. That they cannot is asserted rather than
// assumed: the guard in this package drives the broker's own admission decision
// and selection's offer decision across the same boundary and requires them to
// agree at every point.
func SelectionReserve() selection.Reserve {
	r := selection.DefaultReserve()
	r.AcceleratorHeadroomBytes = uint64(vrambroker.HeadroomBytes)
	return r
}
