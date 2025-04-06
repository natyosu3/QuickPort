package app

import (
	"QuickPort/screens"

	tea "github.com/charmbracelet/bubbletea"
)

// アプリの状態を管理する Model
type AppModel struct {
	currentScreen tea.Model
}

// 初期画面をセット
func New() AppModel {
	return AppModel{currentScreen: screens.NewWelcomeScreen()}
}

func (m AppModel) Init() tea.Cmd {
	return m.currentScreen.Init()
}

func (m AppModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	newModel, cmd := m.currentScreen.Update(msg)

	// 画面遷移を管理
	if msg, ok := msg.(screens.ScreenChangeMsg); ok {
		switch msg.Screen {
		case "welcome":
			m.currentScreen = screens.NewWelcomeScreen()
		case "create_account":
			m.currentScreen = screens.InitialCreateAccountModel()
		case "generate_token":
			m.currentScreen = screens.InitialGenerateTokenModel()
		case "start_frpc":
			m.currentScreen = screens.InitialStartFrpcModel()
		}
		return m, m.currentScreen.Init() // 新しい画面の Init() を実行
	} else {
		m.currentScreen = newModel
	}
	return m, cmd
}

func (m AppModel) View() string {
	return m.currentScreen.View()
}
