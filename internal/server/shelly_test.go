package server

import (
	"testing"
)

func TestRound3(t *testing.T) {
	cases := []struct {
		in   float64
		want float64
	}{
		{0, 0},
		{1.2345, 1.235},
		{1.2344, 1.234},
		{-800.0, -800.0},
		{-800.1234, -800.123},
		{-800.1235, -800.124},
		{-1.5005, -1.501},
		{1234.5, 1234.5},
	}
	for _, c := range cases {
		got := round3(c.in)
		if got != c.want {
			t.Errorf("round3(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
