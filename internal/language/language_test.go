package language

import "testing"

func TestIsEnglish_RealLeaseText(t *testing.T) {
	text := `This Contract of Lease is entered into by and between the Landlord and the
Tenant. The Tenant shall pay the Landlord a monthly rent of Php 15,000.00,
payable in advance on or before the fifth day of each month.`
	if !IsEnglish(text) {
		t.Errorf("IsEnglish(%q) = false, want true", text)
	}
}

func TestIsEnglish_NonEnglish(t *testing.T) {
	cases := []string{
		"Este Contrato de Arrendamiento se celebra entre el Arrendador y el Arrendatario, quien pagará mensualmente.",
		"Ang Kasunduang ito ay ginawa sa pagitan ng may-ari ng bahay at ng umuupa, na dapat magbayad buwan-buwan.",
	}
	for _, text := range cases {
		t.Run(text, func(t *testing.T) {
			if IsEnglish(text) {
				t.Errorf("IsEnglish(%q) = true, want false", text)
			}
		})
	}
}

func TestIsEnglish_ShortInputPassesThrough(t *testing.T) {
	// Too short for the frequency signal to be meaningful — a short clause
	// snippet shouldn't be falsely rejected.
	if !IsEnglish("Php 15,000/mo") {
		t.Error("short input should pass through as English")
	}
	if !IsEnglish("") {
		t.Error("empty input should pass through as English")
	}
}
