package tui

import (
	"fmt"
	"math"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	green  = lipgloss.Color("10")
	yellow = lipgloss.Color("11")
	blue   = lipgloss.Color("12")
	red    = lipgloss.Color("9")
	dim    = lipgloss.Color("8")
	cyan   = lipgloss.Color("14")
	white  = lipgloss.Color("15")

	stateColors = map[string]lipgloss.Color{
		"LISTENING":  green,
		"THINKING":   yellow,
		"RESPONDING": blue,
	}

	borderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("8"))

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("7"))

	dimStyle    = lipgloss.NewStyle().Foreground(dim)
	userStyle   = lipgloss.NewStyle().Foreground(cyan).Bold(true)
	assistStyle = lipgloss.NewStyle().Foreground(white)
	infoStyle   = lipgloss.NewStyle().Foreground(dim).Italic(true)
)

type Model struct {
	UpdateCh     chan any
	Threshold    float32
	state        string
	audioLevel   float32
	vadProb      float32
	conversation []string
	currentResp  strings.Builder
	width        int
	height       int
}

func NewModel(updateCh chan any, threshold float32) Model {
	return Model{
		UpdateCh:  updateCh,
		Threshold: threshold,
		state:     "LISTENING",
		width:     80,
		height:    24,
	}
}

func (m Model) WaitForUpdate() tea.Msg {
	return <-m.UpdateCh
}

func (m Model) Init() tea.Cmd {
	return m.WaitForUpdate
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case AudioMsg:
		m.audioLevel = msg.Level
		m.vadProb = msg.VADProb
		m.Threshold = msg.Threshold
		return m, m.WaitForUpdate

	case StateMsg:
		m.state = msg.State
		return m, m.WaitForUpdate

	case TranscriptMsg:
		m.conversation = append(m.conversation, userStyle.Render("> "+msg.Text))
		return m, m.WaitForUpdate

	case TokenMsg:
		m.currentResp.WriteString(msg.Token)
		return m, m.WaitForUpdate

	case InfoMsg:
		m.conversation = append(m.conversation, infoStyle.Render(msg.Text))
		return m, m.WaitForUpdate

	case ResponseDoneMsg:
		if m.currentResp.Len() > 0 {
			m.conversation = append(m.conversation, assistStyle.Render(m.currentResp.String()))
			m.currentResp.Reset()
		}
		return m, m.WaitForUpdate
	}

	return m, m.WaitForUpdate
}

func (m Model) View() string {
	contentWidth := m.width - 4 // border padding
	if contentWidth < 20 {
		contentWidth = 20
	}
	barWidth := contentWidth - 18
	if barWidth < 10 {
		barWidth = 10
	}

	// ── Status panel (fixed top) ──
	color, ok := stateColors[m.state]
	if !ok {
		color = dim
	}
	stateStyle := lipgloss.NewStyle().Foreground(color)

	status := fmt.Sprintf(
		" %s %s\n\n %s %s  %s\n %s %s  %s",
		stateStyle.Render("●"),
		stateStyle.Bold(true).Render(m.state),
		dimStyle.Render("Mic"),
		renderBar(m.audioLevel, 1.0, barWidth, green),
		dimStyle.Render(fmt.Sprintf("%.2f", m.audioLevel)),
		dimStyle.Render("VAD"),
		renderVADBar(m.vadProb, m.Threshold, barWidth),
		dimStyle.Render(fmt.Sprintf("%.2f │ thr: %.2f", m.vadProb, m.Threshold)),
	)

	statusBox := borderStyle.
		Width(contentWidth).
		Padding(0, 1).
		Render(status)

	// ── Conversation panel (scrolling) ──
	// Calculate how many lines fit in conversation area
	statusHeight := lipgloss.Height(statusBox)
	convHeight := m.height - statusHeight - 4 // 4 for footer + spacing
	if convHeight < 3 {
		convHeight = 3
	}

	var convLines []string

	// Past conversation
	for _, line := range m.conversation {
		// Word-wrap long lines
		wrapped := wrapText(line, contentWidth-2)
		convLines = append(convLines, wrapped...)
	}

	// Current streaming response
	if m.currentResp.Len() > 0 {
		wrapped := wrapText(assistStyle.Render(m.currentResp.String()), contentWidth-2)
		convLines = append(convLines, wrapped...)
	}

	// Show last N lines that fit
	if len(convLines) > convHeight {
		convLines = convLines[len(convLines)-convHeight:]
	}

	// Pad to fill the panel
	for len(convLines) < convHeight {
		convLines = append(convLines, "")
	}

	convContent := strings.Join(convLines, "\n")

	convBox := borderStyle.
		Width(contentWidth).
		Height(convHeight).
		Padding(0, 1).
		Render(convContent)

	// ── Footer ──
	footer := dimStyle.Render("  ctrl+c to quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		statusBox,
		convBox,
		footer,
	)
}

func renderBar(value, max float32, width int, color lipgloss.Color) string {
	if value < 0 {
		value = 0
	}
	if value > max {
		value = max
	}
	filled := int(float32(width) * value / max)
	if filled > width {
		filled = width
	}

	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("▓", filled)) +
		lipgloss.NewStyle().Foreground(dim).Render(strings.Repeat("░", width-filled))
}

func renderVADBar(prob, threshold float32, width int) string {
	threshPos := int(float32(width) * threshold)
	if threshPos >= width {
		threshPos = width - 1
	}
	filled := int(float32(width) * prob)
	if filled > width {
		filled = width
	}

	var bar strings.Builder
	for i := 0; i < width; i++ {
		if i == threshPos {
			bar.WriteString(lipgloss.NewStyle().Foreground(red).Bold(true).Render("│"))
		} else if i < filled {
			if prob >= threshold {
				bar.WriteString(lipgloss.NewStyle().Foreground(green).Render("▓"))
			} else {
				bar.WriteString(lipgloss.NewStyle().Foreground(yellow).Render("▓"))
			}
		} else {
			bar.WriteString(lipgloss.NewStyle().Foreground(dim).Render("░"))
		}
	}
	return bar.String()
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	// Simple wrap by runes (lipgloss-aware width would be better but this works)
	var lines []string
	for len(text) > 0 {
		if len(text) <= width {
			lines = append(lines, text)
			break
		}
		// Find last space before width
		cut := width
		for cut > 0 && text[cut] != ' ' {
			cut--
		}
		if cut == 0 {
			cut = width // no space found, hard cut
		}
		lines = append(lines, text[:cut])
		text = strings.TrimLeft(text[cut:], " ")
	}
	return lines
}

// RMS computes root-mean-square amplitude of int16 samples, normalized to 0.0-1.0.
func RMS(samples []int16) float32 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		sum += float64(s) * float64(s)
	}
	rms := math.Sqrt(sum / float64(len(samples)))
	return float32(rms / 32768.0)
}
