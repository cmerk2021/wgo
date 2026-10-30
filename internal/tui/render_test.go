package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cmerk2021/wgo/internal/config"
	"github.com/cmerk2021/wgo/internal/execx"
	"github.com/cmerk2021/wgo/internal/monitor"
	"github.com/cmerk2021/wgo/internal/wg"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	cfg := config.Defaults()
	st := &config.State{}
	runner := execx.NewMock()
	client := wg.New(runner)
	ctrl := monitor.NewController(cfg, client, runner)
	return New(cfg, st, ctrl, client)
}

func sized(m *Model, w, h int) *Model {
	mm, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return mm.(*Model)
}

func TestViewTooSmall(t *testing.T) {
	m := sized(testModel(t), 30, 8)
	if !strings.Contains(m.View(), "too small") {
		t.Error("expected too-small message")
	}
}

func TestViewIdleDashboard(t *testing.T) {
	m := sized(testModel(t), 100, 30)
	v := m.View()
	if !strings.Contains(v, "DISCONNECTED") {
		t.Errorf("expected idle state in view:\n%s", v)
	}
	if !strings.Contains(v, "Dashboard") || !strings.Contains(v, "Settings") {
		t.Error("expected tab bar")
	}
}

func TestTabSwitching(t *testing.T) {
	m := sized(testModel(t), 100, 30)
	for i, key := range []string{"1", "2", "3", "4", "5"} {
		mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)})
		m = mm.(*Model)
		if m.tab != Tab(i) {
			t.Errorf("key %s: tab = %d, want %d", key, m.tab, i)
		}
		if m.View() == "" {
			t.Errorf("tab %d renders empty", i)
		}
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = mm.(*Model)
	if m.tab != TabDashboard {
		t.Errorf("tab wraps to dashboard, got %d", m.tab)
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	m = mm.(*Model)
	if m.tab != TabSettings {
		t.Errorf("shift+tab wraps back, got %d", m.tab)
	}
}

func TestHelpOverlay(t *testing.T) {
	m := sized(testModel(t), 100, 30)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	m = mm.(*Model)
	if !strings.Contains(m.View(), "no background daemon") {
		t.Error("expected help overlay with daemon note")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	m = mm.(*Model)
	if strings.Contains(m.View(), "close this help") {
		t.Error("help should close on any key")
	}
}

func TestQuitFlowWithoutKillswitch(t *testing.T) {
	m := sized(testModel(t), 100, 30)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("expected quit command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("kill switch off: q must quit immediately")
	}
}
