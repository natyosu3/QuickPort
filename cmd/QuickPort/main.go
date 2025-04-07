package main

import (
	"fmt"
	"os"

	"QuickPort/app"
	"QuickPort/internal/update"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// updateを実行する
	err := update.DownloadAndBuildLatestRelease()
	if err != nil {
		fmt.Printf("Update failed: %v\n", err)
		return
	}

	p := tea.NewProgram(app.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("エラーが発生しました: %v", err)
		os.Exit(1)
	}
}
