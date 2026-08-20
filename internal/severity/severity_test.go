package severity

import "testing"

func TestCheck_PhraseRulesTrigger(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		wantIssue string
	}{
		{
			name:      "self-help eviction",
			text:      "In case of default, the Landlord may change the locks and remove the tenant's belongings without a court order.",
			wantIssue: "self-help eviction / lockout clause",
		},
		{
			name:      "deposit forfeiture",
			text:      "The security deposit is non-refundable and shall be forfeited for any reason upon termination.",
			wantIssue: "blanket deposit forfeiture",
		},
		{
			name:      "habitability waiver",
			text:      "The unit is leased on an as-is where-is basis and the Landlord is not obligated to make any repairs.",
			wantIssue: "waiver of habitability / repair obligations",
		},
		{
			name:      "venue/remedy waiver",
			text:      "The Tenant waives all legal remedies and venue shall lie exclusively in Makati City.",
			wantIssue: "waiver of legal remedies / improper venue",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Check(c.text)
			if len(got) == 0 {
				t.Fatalf("Check(%q) returned no findings, want one matching %q", c.text, c.wantIssue)
			}
			found := false
			for _, f := range got {
				if f.Issue == c.wantIssue {
					found = true
					if f.Severity != "HIGH" {
						t.Errorf("Severity = %q, want HIGH", f.Severity)
					}
					if f.ClauseText != c.text {
						t.Errorf("ClauseText = %q, want original clause text", f.ClauseText)
					}
				}
			}
			if !found {
				t.Errorf("Check(%q) = %+v, want an issue named %q", c.text, got, c.wantIssue)
			}
		})
	}
}

func TestCheck_BenignClauseDoesNotTrigger(t *testing.T) {
	benign := []string{
		"The lease term is for one (1) year, renewable upon mutual agreement of both parties.",
		"The Tenant shall pay a security deposit of two (2) months and one (1) month advance rent.",
		"Rent may be increased by five percent (5%) upon renewal, subject to written notice.",
		"Unpaid rent shall accrue interest of two percent (2%) per month.",
		"",
	}
	for _, text := range benign {
		t.Run(text, func(t *testing.T) {
			if got := Check(text); len(got) != 0 {
				t.Errorf("Check(%q) = %+v, want no findings", text, got)
			}
		})
	}
}

func TestCheckDepositCap_ExceedsAdvance(t *testing.T) {
	got := Check("The Tenant shall pay six (6) months advance rent upon signing.")
	if len(got) != 1 || got[0].Issue != "advance rent exceeds Rent Control Act cap" {
		t.Fatalf("got %+v, want one 'advance rent exceeds' finding", got)
	}
}

func TestCheckDepositCap_ExceedsDeposit(t *testing.T) {
	got := Check("A security deposit of 5 months is required before move-in.")
	if len(got) != 1 || got[0].Issue != "security deposit exceeds Rent Control Act cap" {
		t.Fatalf("got %+v, want one 'security deposit exceeds' finding", got)
	}
}

func TestCheckDepositCap_WithinCapDoesNotTrigger(t *testing.T) {
	got := Check("The Tenant shall pay 1 month advance and a 2 months deposit.")
	if len(got) != 0 {
		t.Errorf("got %+v, want no findings for in-cap amounts", got)
	}
}

func TestCheckDepositCap_ParentheticalDigitStyle(t *testing.T) {
	// "six (6) months" is a common PH contract convention (spelled-out
	// word immediately followed by the parenthetical digit).
	got := Check("The Tenant shall pay six (6) months advance rent upon signing.")
	if len(got) != 1 || got[0].Issue != "advance rent exceeds Rent Control Act cap" {
		t.Fatalf("got %+v, want one 'advance rent exceeds' finding", got)
	}
}

func TestCheckRentIncreaseCap_ExceedsCap(t *testing.T) {
	got := Check("The Landlord may increase the rent by 15% upon each annual renewal.")
	if len(got) != 1 || got[0].Issue != "rent increase exceeds Rent Control Act cap" {
		t.Fatalf("got %+v, want one 'rent increase exceeds' finding", got)
	}
}

func TestCheckPenaltyInterest_DailyExceedsThreshold(t *testing.T) {
	got := Check("Late payments shall incur a penalty of 3% per day until fully paid.")
	if len(got) != 1 || got[0].Issue != "potentially unconscionable penalty/interest rate" {
		t.Fatalf("got %+v, want one penalty/interest finding", got)
	}
}

func TestCheckPenaltyInterest_MonthlyWithinThresholdDoesNotTrigger(t *testing.T) {
	got := Check("Unpaid balances accrue interest of 3% per month.")
	if len(got) != 0 {
		t.Errorf("got %+v, want no findings for 3%% monthly (below threshold)", got)
	}
}

func TestCheck_MultipleFindingsInOneClause(t *testing.T) {
	text := "The deposit shall be forfeited for any reason, and the Tenant shall pay 6 months advance rent."
	got := Check(text)
	if len(got) != 2 {
		t.Fatalf("got %d findings, want 2 (forfeiture + advance cap): %+v", len(got), got)
	}
}
