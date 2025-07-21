package main

import (
	"fmt"
	"log"
	"os"

	"QuickPort/app"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	// ログファイルを作成または開く
	logFile, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err != nil {
		fmt.Println("Failed to open log file:", err)
		return
	}
	defer logFile.Close()

	// ログの出力先をファイルに設定
	log.SetOutput(logFile)

	// ログのフォーマットを設定
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	p := tea.NewProgram(app.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("エラーが発生しました: %v", err)
		os.Exit(1)
	}
}
