package main

import (
	"fmt"
	"log"
	"os"

	"QuickPort/app"
	"QuickPort/internal/update"
	"QuickPort/internal/util"
	"QuickPort/share"

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


	// 古いバージョンの削除
	if err := update.DeleteOldVersion(); err != nil {
		fmt.Printf("Failed to delete old version: %v\n", err)
		return
	}

	// バージョンチェックを行う
	newVer, err := util.GetNewVersion(share.VERSION)
	if err != nil {
		fmt.Printf("Version check failed: %v\n", err)
		return
	}

	if newVer != "" {
		fmt.Printf("新しいバージョン %s が利用可能です。\n", newVer)
		fmt.Println("自動更新を実行します。")
		// updateを実行する
		err = update.DownloadAndBuildLatestRelease()
		if err != nil {
			fmt.Printf("Update failed: %v\n", err)
			return
		}
	} else {
		fmt.Println("最新のバージョンです。")
	}

	p := tea.NewProgram(app.New(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("エラーが発生しました: %v", err)
		os.Exit(1)
	}
}
