package control

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/HelixDevelopment/HelixLLM/internal/shared/i18n"
)

// resolveMonitorLang picks a 2-letter language tag for the cluster
// monitor's user-facing strings using the standard POSIX env
// precedence (LC_ALL > LANG). Falls back to "en".
//
// Decoupling per CONST-051(B): the control package reads only standard
// POSIX locale env vars — it carries no consumer-project context and
// remains reusable as a standalone library.
func resolveMonitorLang() string {
	for _, env := range []string{"LC_ALL", "LANG"} {
		if v := os.Getenv(env); len(v) >= 2 {
			return v[:2]
		}
	}
	return "en"
}

// RunMonitor displays a refreshing cluster-status table on stdout until
// ctx is cancelled or an unrecoverable error occurs.  interval controls
// how often the display is refreshed.
//
// CONST-046 round-321: all user-facing strings are resolved through the
// i18n Translator so non-English operators get localised output.
func RunMonitor(ctx context.Context, cp *ControlPlane, interval time.Duration) error {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lang := resolveMonitorLang()
	tr := i18n.New(lang)

	// Render once immediately so the user sees output right away.
	renderStatus(cp, tr, lang)

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			renderStatus(cp, tr, lang)
		}
	}
}

// renderStatus clears the terminal and prints the current cluster
// status table, localising every user-facing string via tr.
func renderStatus(cp *ControlPlane, tr i18n.TranslatorAPI, lang string) {
	// ANSI: move cursor to home, clear screen.
	fmt.Print("\033[H\033[2J")
	fmt.Println(tr.T(lang, i18n.KeyMonitorTitle))
	fmt.Println(tr.T(lang, i18n.KeyMonitorTitleRule))
	fmt.Println()

	status := cp.Status()

	if len(status.Hosts) == 0 {
		fmt.Println(tr.T(lang, i18n.KeyMonitorNoHosts))
		fmt.Printf("\n%s\n", tr.T(lang, i18n.KeyMonitorLastCheck, map[string]string{
			"time": status.CheckedAt.Format(time.RFC3339),
		}))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
		tr.T(lang, i18n.KeyMonitorColHost),
		tr.T(lang, i18n.KeyMonitorColStatus),
		tr.T(lang, i18n.KeyMonitorColCPUCores),
		tr.T(lang, i18n.KeyMonitorColMemoryMB),
		tr.T(lang, i18n.KeyMonitorColDeploys),
	)
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

	overall := tr.T(lang, i18n.KeyMonitorOverallOK)
	if !status.Healthy {
		overall = tr.T(lang, i18n.KeyMonitorOverallBad)
	}
	fmt.Printf("\n%s\n", tr.T(lang, i18n.KeyMonitorClusterState, map[string]string{
		"overall": overall,
		"hosts":   strconv.Itoa(len(status.Hosts)),
		"time":    status.CheckedAt.Format(time.RFC3339),
	}))
}
