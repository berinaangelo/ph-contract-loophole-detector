package redflag

import "testing"

func TestCheck_Triggers(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		wantRule string
	}{
		{
			name:     "cardiac",
			text:     "I've had crushing chest pain for the last hour and I'm having trouble breathing.",
			wantRule: "possible heart attack",
		},
		{
			name:     "stroke",
			text:     "My mother suddenly has slurred speech and her face is drooping on one side.",
			wantRule: "possible stroke (FAST signs)",
		},
		{
			name:     "anaphylaxis",
			text:     "After the bee sting my throat is closing and I can't breathe.",
			wantRule: "anaphylaxis / severe allergic reaction",
		},
		{
			name:     "severe bleeding",
			text:     "I cut my leg on some glass and it won't stop bleeding.",
			wantRule: "severe / uncontrolled bleeding",
		},
		{
			name:     "loss of consciousness",
			text:     "He fell down the stairs and passed out, he's unresponsive now.",
			wantRule: "loss of consciousness",
		},
		{
			name:     "respiratory distress",
			text:     "I'm gasping for air just sitting here and my lips are turning blue.",
			wantRule: "severe difficulty breathing at rest",
		},
		{
			name:     "thunderclap headache",
			text:     "This is the worst headache of my life, it came on all at once.",
			wantRule: "thunderclap headache",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Check(c.text)
			if !got.Triggered {
				t.Fatalf("Check(%q).Triggered = false, want true (rule %q)", c.text, c.wantRule)
			}
			if got.RuleName != c.wantRule {
				t.Errorf("Check(%q).RuleName = %q, want %q", c.text, got.RuleName, c.wantRule)
			}
		})
	}
}

func TestCheck_DoesNotTrigger(t *testing.T) {
	benign := []string{
		"I have a mild headache and feel tired.",
		"I've had a low-grade fever and sore throat since yesterday.",
		"My stomach has felt a bit off after lunch.",
		"I hurt my arm gardening and it's a little sore.",
		"I get out of breath after climbing several flights of stairs.",
		"I have chest pain when I press on a bruise from a fall.",
		"",
	}

	for _, text := range benign {
		t.Run(text, func(t *testing.T) {
			got := Check(text)
			if got.Triggered {
				t.Errorf("Check(%q) = %+v, want Triggered = false", text, got)
			}
		})
	}
}

// TestCheck_AvoidsSubstringFalsePositives guards the specific traps
// multi-word phrasing was chosen to avoid (e.g. "arm" alone would match
// inside "alarm").
func TestCheck_AvoidsSubstringFalsePositives(t *testing.T) {
	cases := []string{
		"My alarm went off and startled me.",
		"We live on a small farm and I was out feeding the animals.",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			got := Check(text)
			if got.Triggered {
				t.Errorf("Check(%q) = %+v, want Triggered = false", text, got)
			}
		})
	}
}
