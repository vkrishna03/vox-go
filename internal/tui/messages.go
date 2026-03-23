package tui

// Message types sent from goroutines to the TUI via UpdateCh.

type AudioMsg struct {
	Level     float32 // RMS amplitude 0.0-1.0
	VADProb   float32 // speech probability 0.0-1.0
	Threshold float32 // current VAD threshold (changes during TTS)
}

type StateMsg struct {
	State string // "LISTENING", "THINKING", "RESPONDING"
}

type TranscriptMsg struct {
	Text string // user's final transcript
}

type TokenMsg struct {
	Token string // single LLM response token
}

type InfoMsg struct {
	Text string // status like "[interrupted]"
}

type ResponseDoneMsg struct{} // LLM response finished, add newline
