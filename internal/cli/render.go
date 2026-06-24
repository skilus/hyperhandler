package cli

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/shopspring/decimal"
	"golang.org/x/term"

	"github.com/skilus/hyperhandler/internal/models"
)

// useColor reports whether ANSI styling should be emitted. Honors NO_COLOR and
// requires stdout to be a TTY.
var useColor = colorEnabled()

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

func colorize(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func red(s string) string    { return colorize("31", s) }
func green(s string) string  { return colorize("32", s) }
func yellow(s string) string { return colorize("33", s) }
func cyan(s string) string   { return colorize("36", s) }
func bold(s string) string   { return colorize("1", s) }
func dim(s string) string    { return colorize("2", s) }

// out/errln print to stdout/stderr.
func out(format string, a ...any)   { fmt.Fprintf(os.Stdout, format+"\n", a...) }
func errln(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }

var ansiRE = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func visibleLen(s string) int { return len([]rune(ansiRE.ReplaceAllString(s, ""))) }

// Column describes a table column.
type Column struct {
	Name  string
	Right bool // right-align
}

// Table is a minimal aligned table renderer replacing rich.Table.
type Table struct {
	Title string
	cols  []Column
	rows  [][]string
}

func NewTable(title string) *Table { return &Table{Title: title} }

func (t *Table) Col(name string) *Table {
	t.cols = append(t.cols, Column{Name: name})
	return t
}

func (t *Table) ColRight(name string) *Table {
	t.cols = append(t.cols, Column{Name: name, Right: true})
	return t
}

func (t *Table) Row(cells ...string) *Table {
	t.rows = append(t.rows, cells)
	return t
}

// Render writes the table to stdout.
func (t *Table) Render() {
	widths := make([]int, len(t.cols))
	for i, c := range t.cols {
		widths[i] = visibleLen(c.Name)
	}
	for _, row := range t.rows {
		for i, cell := range row {
			if i < len(widths) {
				if w := visibleLen(cell); w > widths[i] {
					widths[i] = w
				}
			}
		}
	}

	if t.Title != "" {
		out("%s", bold(t.Title))
	}

	header := make([]string, len(t.cols))
	for i, c := range t.cols {
		header[i] = pad(bold(c.Name), widths[i], c.Right)
	}
	out("%s", strings.Join(header, "  "))

	sep := make([]string, len(t.cols))
	for i := range t.cols {
		sep[i] = strings.Repeat("-", widths[i])
	}
	out("%s", dim(strings.Join(sep, "  ")))

	for _, row := range t.rows {
		cells := make([]string, len(t.cols))
		for i := range t.cols {
			val := ""
			if i < len(row) {
				val = row[i]
			}
			cells[i] = pad(val, widths[i], t.cols[i].Right)
		}
		out("%s", strings.Join(cells, "  "))
	}
}

func pad(s string, width int, right bool) string {
	gap := width - visibleLen(s)
	if gap <= 0 {
		return s
	}
	spaces := strings.Repeat(" ", gap)
	if right {
		return spaces + s
	}
	return s + spaces
}

// --- decimal/format helpers mirroring the Python f-string specifiers ---

// fixed renders d with n decimal places (f"{d:.nf}").
func fixed(d decimal.Decimal, n int) string { return d.StringFixed(int32(n)) }

// signedFixed renders d with an explicit sign (f"{d:+.nf}").
func signedFixed(d decimal.Decimal, n int) string {
	s := d.StringFixed(int32(n))
	if d.Sign() >= 0 && !strings.HasPrefix(s, "+") {
		return "+" + s
	}
	return s
}

// pct renders d as a percentage with n decimals (f"{d:.np%}").
func pct(d decimal.Decimal, n int) string {
	return d.Mul(decimal.NewFromInt(100)).StringFixed(int32(n)) + "%"
}

// signedPct renders d as a signed percentage (f"{d:+.np%}").
func signedPct(d decimal.Decimal, n int) string {
	s := d.Mul(decimal.NewFromInt(100)).StringFixed(int32(n))
	if d.Sign() >= 0 && !strings.HasPrefix(s, "+") {
		s = "+" + s
	}
	return s + "%"
}

func shortAddr(a string) string {
	if len(a) <= 12 {
		return a
	}
	return a[:6] + "..." + a[len(a)-4:]
}

func sideLong(isLong bool) string {
	if isLong {
		return green("LONG")
	}
	return red("SHORT")
}

func pnlColored(d decimal.Decimal, decimals int) string {
	s := signedFixed(d, decimals)
	if d.Sign() >= 0 {
		return green(s)
	}
	return red(s)
}

// pnlColoredPct colors an already-scaled percentage value (e.g. 12.5 for
// 12.5%) with an explicit sign and one decimal.
func pnlColoredPct(d decimal.Decimal) string {
	s := signedFixed(d, 1) + "%"
	if d.Sign() >= 0 {
		return green(s)
	}
	return red(s)
}

// truncate shortens s to at most n runes (no ellipsis), matching the Python
// slice truncation used in the vault tables.
func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// --- summary printers (mirror cli.py _print_* helpers) ---

func printSignalSummary(s *models.TradingSignal) {
	out("")
	out("  Pair: %s", cyan(s.Pair))
	if s.IsBuy() {
		out("  Side: %s", green("LONG"))
	} else {
		out("  Side: %s", red("SHORT"))
	}
	out("  Type: %s", s.OrderType)
	out("  Size: %s", s.Size)
	out("  Leverage: %dx", s.Leverage)
	if s.EntryPrice != nil {
		out("  Entry Price: %s", s.EntryPrice)
	}
	if s.StopLoss != nil {
		out("  Stop Loss: %s", s.StopLoss)
	}
	if s.TakeProfit != nil {
		out("  Take Profit: %s", s.TakeProfit)
	}
}

func printRiskDiff(s *models.TradingSignal, o *models.TradeOrder) {
	out("\n%s", bold("Risk-Managed Adjustments:"))
	tbl := NewTable("")
	tbl.Col("Parameter").Col("Signal").Col("Calculated")
	slSignal := "-"
	if s.StopLoss != nil {
		slSignal = s.StopLoss.String()
	}
	tbl.Row("Size", s.Size.String(), o.Size.String())
	tbl.Row("Leverage", fmt.Sprintf("%dx", s.Leverage), fmt.Sprintf("%dx", o.Leverage))
	tbl.Row("Stop-Loss", slSignal, fixed(o.StopLoss, 2))
	tbl.Row("Est. Liquidation", "-", fixed(o.EstimatedLiquidation, 2))
	tbl.Row("Risk %", "-", pct(o.RiskPct, 2))
	tbl.Row("Margin Required", "-", "$"+fixed(o.MarginRequired, 2))
	tbl.Row("Margin Mode", "-", o.MarginMode)
	tbl.Render()
}

func printTradeOrderSummary(o *models.TradeOrder) {
	out("")
	out("  Pair: %s", cyan(o.Coin))
	if o.Side == "long" {
		out("  Side: %s", green("LONG"))
	} else {
		out("  Side: %s", red("SHORT"))
	}
	out("  Size: %s", o.Size)
	out("  Entry Price: %s", o.EntryPrice)
	out("  Leverage: %dx", o.Leverage)
	out("  Stop Loss: %s", o.StopLoss)
	out("  Risk Amount: $%s", fixed(o.RiskAmount, 2))
	out("  Risk %%: %s", pct(o.RiskPct, 2))
	out("  Cumulative Risk: %s", pct(o.CumulativeRiskAfter, 2))
	out("  Est. Liquidation: %s", fixed(o.EstimatedLiquidation, 2))
	out("  Margin Required: $%s", fixed(o.MarginRequired, 2))
	out("  Est. Commission: $%s", fixed(o.EstimatedCommission, 4))
	out("  Mode: %s (size: %s, sl: %s)", o.RiskMode, o.SizeSource, o.SLSource)
}
