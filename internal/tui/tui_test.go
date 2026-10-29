package tui

import "testing"

func TestQuitDecision(t *testing.T) {
	if QuitDecision(false) != QuitNow {
		t.Error("kill switch off: q must quit immediately")
	}
	if QuitDecision(true) != QuitNeedsConfirm {
		t.Error("kill switch on: q must require confirmation")
	}
}

func TestConfirmQuit(t *testing.T) {
	for _, yes := range []string{"y", "Y"} {
		if !ConfirmQuit(yes) {
			t.Errorf("%q must confirm", yes)
		}
	}
	for _, no := range []string{"n", "N", "", "enter", "esc", "q", " "} {
		if ConfirmQuit(no) {
			t.Errorf("%q must not confirm", no)
		}
	}
}

func TestSparkline(t *testing.T) {
	if s := Sparkline(nil, 5); len([]rune(s)) != 5 {
		t.Errorf("empty sparkline width: %q", s)
	}
	s := Sparkline([]float64{0, 1, 2, 3}, 4)
	if len([]rune(s)) != 4 {
		t.Errorf("width: %q", s)
	}
	if Sparkline([]float64{1}, 0) != "" {
		t.Error("zero width must be empty")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		0:          "0 B",
		512:        "512 B",
		2048:       "2.0 KB",
		3 << 20:    "3.0 MB",
		5 << 30:    "5.0 GB",
	}
	for in, want := range cases {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
