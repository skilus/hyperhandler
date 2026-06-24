package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/skilus/hyperhandler/internal/models"
)

// captureStdout runs fn with os.Stdout redirected to a pipe and returns what was
// written. useColor is false in tests (see cli_test.go init), so output is plain.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()
	fn()
	w.Close()
	os.Stdout = orig
	return <-done
}

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestTableRenderAlignment(t *testing.T) {
	out := captureStdout(t, func() {
		tbl := NewTable("Positions")
		tbl.Col("Coin").ColRight("Size")
		tbl.Row("BTC", "0.1")
		tbl.Row("ETHEREUM", "12.5")
		tbl.Render()
	})

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	// title, header, separator, 2 rows.
	if len(lines) != 5 {
		t.Fatalf("got %d lines, want 5:\n%s", len(lines), out)
	}
	if lines[0] != "Positions" {
		t.Errorf("title line = %q", lines[0])
	}
	// "ETHEREUM" (8) widens the Coin column; "BTC" must be left-padded to 8.
	if !strings.HasPrefix(lines[3], "BTC     ") {
		t.Errorf("BTC row not padded to column width: %q", lines[3])
	}
	// Size column is right-aligned: "0.1" padded on the left to width of "Size"(4).
	if !strings.HasSuffix(lines[3], " 0.1") {
		t.Errorf("size cell not right-aligned: %q", lines[3])
	}
}

func TestTableMissingCellsTolerated(t *testing.T) {
	out := captureStdout(t, func() {
		tbl := NewTable("")
		tbl.Col("A").Col("B")
		tbl.Row("only-a") // short row, B cell missing
		tbl.Render()
	})
	if !strings.Contains(out, "only-a") {
		t.Errorf("expected row content, got %q", out)
	}
}

func TestVisibleLenStripsANSI(t *testing.T) {
	colored := "\x1b[31mERR\x1b[0m"
	if got := visibleLen(colored); got != 3 {
		t.Errorf("visibleLen(%q) = %d, want 3", colored, got)
	}
}

func TestPad(t *testing.T) {
	if got := pad("ab", 5, false); got != "ab   " {
		t.Errorf("left pad = %q", got)
	}
	if got := pad("ab", 5, true); got != "   ab" {
		t.Errorf("right pad = %q", got)
	}
	if got := pad("toolong", 3, false); got != "toolong" {
		t.Errorf("overflow should be untouched, got %q", got)
	}
}

func TestFormatHelpers(t *testing.T) {
	if got := fixed(dec("1.5"), 3); got != "1.500" {
		t.Errorf("fixed = %q", got)
	}
	if got := signedPct(dec("0.025"), 1); got != "+2.5%" {
		t.Errorf("signedPct = %q, want +2.5%%", got)
	}
	if got := signedPct(dec("-0.01"), 1); got != "-1.0%" {
		t.Errorf("signedPct neg = %q", got)
	}
	// useColor=false → pnlColored/sideLong return plain strings.
	if got := pnlColored(dec("-3"), 2); got != "-3.00" {
		t.Errorf("pnlColored = %q", got)
	}
	if got := pnlColoredPct(dec("4.5")); got != "+4.5%" {
		t.Errorf("pnlColoredPct = %q", got)
	}
	if got := sideLong(true); got != "LONG" {
		t.Errorf("sideLong(true) = %q", got)
	}
	if got := sideLong(false); got != "SHORT" {
		t.Errorf("sideLong(false) = %q", got)
	}
}

func TestPrintSignalSummary(t *testing.T) {
	sig, err := models.NewTradingSignal(models.SignalParams{
		Pair:       "BTC",
		Side:       models.Long,
		OrderType:  models.Market,
		Size:       dec("0.5"),
		Leverage:   10,
		StopLoss:   models.Ptr(dec("49000")),
		TakeProfit: models.Ptr(dec("55000")),
	})
	if err != nil {
		t.Fatal(err)
	}
	out := captureStdout(t, func() { printSignalSummary(sig) })
	for _, want := range []string{"Pair: BTC", "Side: LONG", "Leverage: 10x", "Stop Loss: 49000", "Take Profit: 55000"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q:\n%s", want, out)
		}
	}
}

func sampleOrder() *models.TradeOrder {
	return &models.TradeOrder{
		Coin: "BTC", Side: "long", Size: dec("0.1"), EntryPrice: dec("50000"),
		Leverage: 5, MarginMode: "cross",
		StopLoss: dec("49000"), RiskAmount: dec("100"), RiskPct: dec("0.01"),
		CumulativeRiskAfter:  dec("0.02"),
		EstimatedLiquidation: dec("45000"),
		EstimatedCommission:  dec("0.5"), MarginRequired: dec("1000"),
		RiskMode: models.ModeManaged, SizeSource: "calculated", SLSource: "signal",
	}
}

func TestPrintRiskDiff(t *testing.T) {
	sig, _ := models.NewTradingSignal(models.SignalParams{
		Pair: "BTC", Side: models.Long, OrderType: models.Market,
		Size: dec("0.2"), Leverage: 10, StopLoss: models.Ptr(dec("48000")),
	})
	out := captureStdout(t, func() { printRiskDiff(sig, sampleOrder()) })
	for _, want := range []string{"Risk-Managed Adjustments", "Size", "Leverage", "10x", "5x", "Stop-Loss", "Margin Mode", "cross"} {
		if !strings.Contains(out, want) {
			t.Errorf("risk diff missing %q:\n%s", want, out)
		}
	}
}

func TestPrintTradeOrderSummary(t *testing.T) {
	out := captureStdout(t, func() { printTradeOrderSummary(sampleOrder()) })
	for _, want := range []string{"Pair: BTC", "Side: LONG", "Leverage: 5x", "Risk Amount: $100.00", "Mode: managed"} {
		if !strings.Contains(out, want) {
			t.Errorf("order summary missing %q:\n%s", want, out)
		}
	}
}
