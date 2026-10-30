package tui

// QuitAction is the decision when q is pressed.
type QuitAction int

const (
	// QuitNow exits immediately, leaving the tunnel running.
	QuitNow QuitAction = iota
	// QuitNeedsConfirm shows the kill-switch removal confirmation.
	QuitNeedsConfirm
)

// QuitDecision implements the dynamic quit behavior: with the kill switch
// active, quitting requires confirmation (rules will be removed, tunnel
// stays up); otherwise quit immediately.
func QuitDecision(killswitchActive bool) QuitAction {
	if killswitchActive {
		return QuitNeedsConfirm
	}
	return QuitNow
}

// ConfirmQuit interprets a key press on the quit confirmation prompt.
// Returns proceed=true only for an explicit yes.
func ConfirmQuit(key string) (proceed bool) {
	return key == "y" || key == "Y"
}
