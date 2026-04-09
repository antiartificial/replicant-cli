package tui

import (
	"encoding/base64"
	"fmt"
	"os/exec"
	"runtime"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// clipboardResultMsg is sent after a clipboard copy attempt.
type clipboardResultMsg struct {
	err error
}

// copyToClipboard returns a bubbletea Cmd that copies text to the system
// clipboard. It tries the platform clipboard command first (pbcopy on macOS,
// xclip/xsel on Linux, clip.exe on Windows). Falls back to OSC 52 escape
// sequence which works in most modern terminals.
func copyToClipboard(text string) tea.Cmd {
	return func() tea.Msg {
		err := writeClipboard(text)
		return clipboardResultMsg{err: err}
	}
}

func writeClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		if path, err := exec.LookPath("xclip"); err == nil {
			cmd = exec.Command(path, "-selection", "clipboard")
		} else if path, err := exec.LookPath("xsel"); err == nil {
			cmd = exec.Command(path, "--clipboard", "--input")
		}
	case "windows":
		cmd = exec.Command("clip.exe")
	}

	if cmd != nil {
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		}
	}

	// OSC 52 escape sequence fallback.
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	fmt.Printf("\033]52;c;%s\a", encoded)
	return nil
}
