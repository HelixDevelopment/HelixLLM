package control

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"
)

// RunMonitor displays a refreshing cluster-status table on stdout until
// ctx is cancelled or an unrecoverable error occurs.  interval controls
// how often the display is refreshed.
func RunMonitor(ctx context.Context, cp *ControlPlane, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Render once immediately so the user sees output right away.
	renderStatus(cp)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			renderStatus(cp)
		}
	}
}

// renderStatus clears the terminal and prints the current cluster
// status table.
func renderStatus(cp *ControlPlane) {
	// ANSI: move cursor to home, clear screen.
	fmt.Print("\033[H\033[2J")
	fmt.Println("HelixLLM Cluster Monitor")
	fmt.Println("========================")
	fmt.Println()

	status := cp.Status()

	if len(status.Hosts) == 0 {
		fmt.Println("No hosts configured.")
		fmt.Printf("\nLast check: %s\n", status.CheckedAt.Format(time.RFC3339))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "HOST\tSTATUS\tCPU CORES\tMEMORY (MB)\tDEPLOYMENTS\n")
	fmt.Fprintf(w, "----\t------\t---------\t-----------\t-----------\n")

	// Count deployments per host for the last column.
	depCount := make(map[string]int, len(status.Hosts))
	for _, d := range status.Deployments {
		depCount[d.HostName]++
	}

	for _, host := range status.Hosts {
		memMB := host.Memory.TotalBytes / (1024 * 1024)
		fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%d\n",
			host.Name,
			host.State,
			host.CPU.Cores,
			memMB,
			depCount[host.Name],
		)
	}
	w.Flush()

	overall := "healthy"
	if !status.Healthy {
		overall = "DEGRADED"
	}
	fmt.Printf("\nCluster: %s  |  Hosts: %d  |  Last check: %s\n",
		overall,
		len(status.Hosts),
		status.CheckedAt.Format(time.RFC3339),
	)
}
