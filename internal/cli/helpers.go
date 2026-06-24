package cli

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/skilus/hyperhandler/internal/models"
)

// optionalFlag returns a pointer to the flag's string value, or nil when the
// flag was not set (empty), mirroring the Optional[str] = None defaults in the
// Python CLI.
func optionalFlag(cmd *cobra.Command, name string) *string {
	v, _ := cmd.Flags().GetString(name)
	if v == "" {
		return nil
	}
	return &v
}

// parseRiskLevel parses a risk level case-insensitively. Mirrors
// RiskLevel(value.lower()) in the Python CLI.
func parseRiskLevel(s string) (models.RiskLevel, bool) {
	switch strings.ToLower(s) {
	case "low":
		return models.RiskLow, true
	case "medium":
		return models.RiskMedium, true
	case "high":
		return models.RiskHigh, true
	}
	return "", false
}

func itoa(n int) string     { return strconv.Itoa(n) }
func i64toa(n int64) string { return strconv.FormatInt(n, 10) }

// orderLabel maps a result index to a human label, matching the Python
// "Entry"/"Stop-Loss"/"Take-Profit" sequencing.
func orderLabel(i int, hasStopLoss bool) string {
	switch {
	case i == 0:
		return "Entry"
	case i == 1 && hasStopLoss:
		return "Stop-Loss"
	default:
		return "Take-Profit"
	}
}

// mapToString renders a map deterministically for display in fallback messages.
func mapToString(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return "{" + strings.Join(parts, ", ") + "}"
}
