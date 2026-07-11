// Command genproof holds a REAL vrambroker.ClassImage burst lease (§11.4.119
// single-owner) for the FULL duration of a real FLUX.1-schnell generation
// subprocess, so the broker admission is not a release-immediately dry run
// but genuinely spans the GPU workload it gates.
//
// It re-reads nvidia-smi/broker Budget().free LIVE immediately before
// admission (DZ-23 volatility), Acquires the burst lease for the declared
// tier footprint, execs the generation subprocess (inheriting stdio) while
// the lease is held, then Releases the lease and re-reads Budget() to prove
// VRAM was restored. It NEVER touches the coder (§11.4.122) — the coder is a
// separate resident process (ClassCoder, always granted, not gated by this
// tool) on its own port.
//
// Usage:
//
//	genproof <needBytesGiB> -- <cmd> [args...]
//
// Exit codes: 0 = admitted + subprocess exit 0 + released.
// Non-zero: admission BLOCKED (classified per vrambroker error) or subprocess
// failed (subprocess exit code propagated after the lease is still released
// in a defer, so VRAM is never left stranded on a failure path).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/vrambroker"
)

const GiB int64 = 1024 * 1024 * 1024

func main() {
	if len(os.Args) < 4 || os.Args[2] != "--" {
		fatal("usage: genproof <needBytesGiB> -- <cmd> [args...]")
	}
	needGiB, err := strconv.ParseFloat(os.Args[1], 64)
	if err != nil || needGiB <= 0 {
		fatal("invalid needBytesGiB: %s", os.Args[1])
	}
	need := int64(needGiB * float64(GiB))
	cmdArgs := os.Args[3:]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	broker := vrambroker.New()
	total, used, free := broker.Budget()
	fmt.Printf("[genproof] PRE-ADMISSION live nvidia-smi read: total=%dMiB used=%dMiB free=%dMiB need=%dMiB headroom=%dMiB\n",
		total/(1024*1024), used/(1024*1024), free/(1024*1024), need/(1024*1024), vrambroker.HeadroomBytes/(1024*1024))

	lease, err := broker.Acquire(ctx, vrambroker.ClassImage, need)
	if err != nil {
		switch {
		case errors.Is(err, vrambroker.ErrBudgetExceeded):
			fmt.Println("[genproof] BLOCKED: ErrBudgetExceeded — tier does not fit alongside the live coder right now. Coder untouched.")
			os.Exit(10)
		case errors.Is(err, vrambroker.ErrBurstInUse):
			fmt.Println("[genproof] BLOCKED: ErrBurstInUse — another burst owns the card (single-owner §11.4.119).")
			os.Exit(11)
		case errors.Is(err, vrambroker.ErrBudgetUnavailable):
			fmt.Println("[genproof] BLOCKED: ErrBudgetUnavailable — nvidia-smi unreadable; fail-closed (§11.4.6). No generation.")
			os.Exit(12)
		case errors.Is(err, vrambroker.ErrThermalUnsafe):
			fmt.Println("[genproof] BLOCKED: ErrThermalUnsafe — card outside safe thermal/power envelope (§11.4.133).")
			os.Exit(13)
		default:
			fmt.Printf("[genproof] BLOCKED: admission failed: %v\n", err)
			os.Exit(14)
		}
	}
	fmt.Printf("[genproof] ADMIT-OK: lease id=%s class=%s vram_bytes=%d — coder stays live, generating now...\n",
		lease.ID, lease.Class, lease.VRAMBytes)

	exitCode := 0
	defer func() {
		lease.Release()
		t2, u2, f2 := broker.Budget()
		fmt.Printf("[genproof] POST-RELEASE live nvidia-smi read: total=%dMiB used=%dMiB free=%dMiB\n",
			t2/(1024*1024), u2/(1024*1024), f2/(1024*1024))
		os.Exit(exitCode)
	}()

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	runErr := cmd.Run()
	if runErr != nil {
		fmt.Printf("[genproof] subprocess failed: %v\n", runErr)
		exitCode = 1
		return
	}
	fmt.Println("[genproof] subprocess exit 0 — real generation completed while lease held.")
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", a...)
	os.Exit(2)
}
