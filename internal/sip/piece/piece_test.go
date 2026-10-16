package piece

import "testing"

func TestMinutes(t *testing.T) {
	for words, want := range map[int]int{0: 1, 230: 1, 231: 2, 690: 3} {
		if got := (Piece{Words: words}).Minutes(); got != want {
			t.Errorf("Minutes(%d words) = %d, want %d", words, got, want)
		}
	}
}

func TestHeadline(t *testing.T) {
	tests := []struct {
		p    Piece
		want string
	}{
		{Piece{Title: "X", Dek: "D"}, "X"},
		{Piece{Dek: "D"}, "D"},
		{Piece{}, "Untitled"},
	}
	for _, tt := range tests {
		if got := tt.p.Headline(); got != tt.want {
			t.Errorf("Headline(%+v) = %q, want %q", tt.p, got, tt.want)
		}
	}
}
