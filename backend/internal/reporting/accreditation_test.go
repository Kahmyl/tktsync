package reporting

import "testing"

func TestSafeCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"=HYPERLINK(\"https://example.test\")", "+SUM(1,2)", "-1+2", "@SUM(1,2)", "  =1+1"} {
		if got := safeCSV(value); len(got) == 0 || got[0] != '\'' {
			t.Fatalf("safeCSV(%q)=%q", value, got)
		}
	}
	for _, value := range []string{"Ada Lovelace", "VIP", "Ticket - 1"} {
		if got := safeCSV(value); got != value {
			t.Fatalf("safeCSV corrupted %q as %q", value, got)
		}
	}
}
