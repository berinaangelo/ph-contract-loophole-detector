package checklist

import "testing"

func TestLooksLikeLease_RealLeaseText(t *testing.T) {
	text := "The Tenant shall pay the Landlord monthly rent under this lease."
	if !LooksLikeLease(text) {
		t.Errorf("LooksLikeLease(%q) = false, want true", text)
	}
}

func TestLooksLikeLease_OffTopicText(t *testing.T) {
	cases := []string{
		"Buy milk, eggs, bread, and coffee at the store this weekend.",
		"The quarterly sales report shows revenue grew twelve percent.",
		"",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			if LooksLikeLease(text) {
				t.Errorf("LooksLikeLease(%q) = true, want false", text)
			}
		})
	}
}

func TestLooksLikeLease_SingleMarkerNotEnough(t *testing.T) {
	// Only one lease-marker word ("rent") — below minLeaseMarkers, so this
	// alone shouldn't be treated as lease-shaped input.
	text := "We need to rent a car for the trip next week."
	if LooksLikeLease(text) {
		t.Errorf("LooksLikeLease(%q) = true, want false (only one marker)", text)
	}
}

func TestCheck_FlagsMissingTopics(t *testing.T) {
	// Only mentions rent and term — everything else should be flagged missing.
	text := "The lease term is one year. The Tenant shall pay monthly rent of Php 15,000."
	got := Check(text)

	wantTopics := map[string]bool{
		"repairs and maintenance responsibility":    true,
		"grounds and notice period for termination": true,
		"renewal terms":                           true,
		"dispute resolution / venue":              true,
		"utilities responsibility":                true,
		"subletting / assignment policy":          true,
		"house rules / property use restrictions": true,
	}
	if len(got) != len(wantTopics) {
		t.Fatalf("got %d findings, want %d: %+v", len(got), len(wantTopics), got)
	}
	for _, f := range got {
		if f.Severity != "LOW" {
			t.Errorf("Severity = %q, want LOW", f.Severity)
		}
		if !wantTopics[f.Topic] {
			t.Errorf("unexpected topic %q", f.Topic)
		}
	}
}

func TestCheck_CoveredTopicNotFlagged(t *testing.T) {
	text := `The lease term is one year, renewable upon mutual written agreement thirty (30) days before expiry.
The Tenant shall be responsible for minor repairs; the Landlord shall handle major structural maintenance.
Either party may terminate this lease upon thirty (30) days written notice for material breach.
Any dispute shall first be brought to barangay mediation before any court action; venue for any suit shall be Quezon City.
The Tenant shall pay for electricity and water utilities directly to the provider.
The Tenant may not sublet or assign this lease without the Landlord's written consent.
House rules regarding pets and noise are attached as Annex A.`

	got := Check(text)
	if len(got) != 0 {
		t.Errorf("got %+v, want no findings (all topics covered)", got)
	}
}
