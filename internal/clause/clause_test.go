package clause

import "testing"

func TestSplit_ParagraphsAndNumbering(t *testing.T) {
	text := "1. TERM. This lease runs for one year.\n\n2. RENT. Tenant pays monthly.\n\n\n3. DEPOSIT. Two months upon signing."
	got := Split(text)
	if len(got) != 3 {
		t.Fatalf("got %d clauses, want 3: %+v", len(got), got)
	}
	if got[0].Index != 1 || got[1].Index != 2 || got[2].Index != 3 {
		t.Errorf("indices not sequential: %+v", got)
	}
	if got[1].Text != "2. RENT. Tenant pays monthly." {
		t.Errorf("got[1].Text = %q, unexpected", got[1].Text)
	}
}

func TestSplit_DropsBlankSegments(t *testing.T) {
	got := Split("first\n\n   \n\nsecond\n\n")
	if len(got) != 2 {
		t.Fatalf("got %d clauses, want 2: %+v", len(got), got)
	}
}

func TestSplit_Empty(t *testing.T) {
	if got := Split(""); len(got) != 0 {
		t.Errorf("Split(\"\") = %+v, want empty", got)
	}
}
