package screens

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"QuickPort/internal/core"
	"QuickPort/share"
)

var (
	sFFocusedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	sFBlurredStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	sFCursorStyle         = sFFocusedStyle
	sFNoStyle             = lipgloss.NewStyle()
	sFHelpStyle           = sFBlurredStyle
	sFCursorModeHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	sFFocusedButton = sFFocusedStyle.Render("[ 登録 ]")
	sFBlurredButton = fmt.Sprintf("[ %s ]", sFBlurredStyle.Render("登録"))
)

type StartFrpcModel struct {
	focusIndex      int
	errorMessage    string
	isComp          bool
	spinner         spinner.Model
	loadding        bool
	token           string
	getPortLoading  bool
	getPortCh       chan getPortChan
	getPortIsComp   bool
	allComp         bool
	clientService   *core.FRPClient
	clientStarted   bool
	progress        progress.Model
	currentStep     int
	maxSteps        int
	stepMessages    []string
	connectionTimer int
	showSuccess     bool
	successTimer    int
	errorCh         chan error
	hasError        bool
}

type getPortChan struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Port    int    `json:"port"`
}

type WebhookPayload struct {
	Status  string `json:"status"`
	Message string `json:"message"`
	Port    int    `json:"port"`
}

type tickMsg time.Time
type progressMsg struct {
	step int
}
type errorMsg struct {
	err error
}

func doTick() tea.Cmd {
	return tea.Tick(time.Millisecond*100, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func waitForError(errorCh chan error) tea.Cmd {
	return func() tea.Msg {
		err := <-errorCh
		return errorMsg{err: err}
	}
}

func InitialStartFrpcModel() StartFrpcModel {
	s := spinner.New()
	s.Spinner = spinner.Globe
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 40
	
	m := StartFrpcModel{
		spinner:        s,
		isComp:         false,
		getPortCh:      make(chan getPortChan),
		getPortLoading: false,
		progress:       prog,
		currentStep:    0,
		maxSteps:       4,
		stepMessages:   []string{
			"トークンを検証中...",
			"サーバーに接続中...",
			"ポートを取得中...",
			"ポートを解放中...",
		},
		connectionTimer: 0,
		showSuccess:     false,
		successTimer:    0,
		errorCh:         make(chan error, 1),
		hasError:        false,
	}

	// トークンファイルからトークンを読み取る
	token, err := readTokenFromFile()
	if err != nil {
		log.Printf("トークンの読み取りに失敗しました: %v", err)
		m.errorMessage = "トークンの読み取りに失敗しました"
		m.hasError = true
	}
	m.token = token

	return m
}

func (m StartFrpcModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, doTick(), waitForError(m.errorCh))
}

func (m StartFrpcModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case ScreenChangeMsg:
		// 画面遷移メッセージの場合、そのまま遷移
		if msg.Screen == "welcome" {
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "welcome"}
			}
		}
	
	case errorMsg:
		// エラーが発生した場合
		m.hasError = true
		m.errorMessage = fmt.Sprintf("%v", msg.err)
		
		// エラー監視を再開
		cmds = append(cmds, waitForError(m.errorCh))
		return m, tea.Batch(cmds...)
	
	
	case tickMsg:
		if !m.showSuccess && !m.hasError {
			m.connectionTimer++
			
			// 進捗を自動的に進める
			if !share.IsConnection {
				if m.connectionTimer%15 == 0 && m.currentStep < m.maxSteps-1 {
					m.currentStep++
				}
			} else if !m.showSuccess {
				m.currentStep = m.maxSteps
				m.showSuccess = true
				m.successTimer = 0
			}
		} else if m.showSuccess {
			m.successTimer++
			// 5秒後にメイン画面に戻る
			if m.successTimer >= 50 {
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "welcome"}
				}
			}
		}
		return m, doTick()
	
	case progressMsg:
		if msg.step <= m.maxSteps {
			m.currentStep = msg.step
		}
		return m, nil
	
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit

		case "esc":
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "welcome"}
			}

		// Set focus to next input
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()

			// トークン発行後エンターを押された場合, welcomeページに遷移
			if s == "enter" && m.isComp {
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "generate_token"}
				}
			}
			// Did the user press enter while the submit button was focused?
			// If so, exit.
			if s == "enter" {
				return m, nil
			}

			return m, nil
		}
	}

	// FRPクライアントがまだ起動していない場合のみ起動
	if !m.clientStarted && m.token != "" && !m.hasError {
		// トークンからメタデータを取得し、FRPクライアントを初期化
		// m.clientService = core.NewFRPClient("163.44.96.225:5555", m.token)
		m.clientService = core.NewFRPClient("localhost:5555", m.token)
		go func() {
			err := m.clientService.Start()
			if err != nil {
				select {
				case m.errorCh <- err:
				default:
				}
			}
		}()
		m.clientStarted = true
	}

	return m, tea.Batch(cmds...)
}

func (m StartFrpcModel) View() string {
	var b strings.Builder

	// ヘッダー
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		Border(lipgloss.RoundedBorder()).
		Padding(0, 2).
		MarginBottom(2)
	
	b.WriteString(headerStyle.Render("🚀 QuickPort - FRP接続"))
	b.WriteString("\n\n")

	// トークン表示
	if m.token != "" {
		tokenStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("14")).
			Background(lipgloss.Color("240")).
			Padding(0, 1).
			Italic(true)
		
		maskedToken := m.token[:5] + strings.Repeat("*", 15)
		b.WriteString("🔑 " + tokenStyle.Render("トークン: "+maskedToken))
		b.WriteString("\n\n")
	}

	if m.hasError {
		// エラー表示
		errorBoxStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("160")).
			Border(lipgloss.DoubleBorder()).
			BorderForeground(lipgloss.Color("160")).
			Padding(1, 2).
			MarginTop(1).
			Bold(true)
		
		errorContent := []string{
			"❌ 接続エラーが発生しました",
			"",
			fmt.Sprintf("📋 エラー詳細: %s", m.errorMessage),
			"",
			"� ESCキーでメイン画面に戻れます",
		}
		
		b.WriteString(errorBoxStyle.Render(strings.Join(errorContent, "\n")))
		
	} else if !m.showSuccess {
		// 接続中の表示
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("220")).
			Bold(true)
		
		b.WriteString(loadingStyle.Render("🔄 接続処理中..."))
		b.WriteString("\n\n")
		
		// スピナーと現在のステップ
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))
		
		b.WriteString(spinnerStyle.Render(m.spinner.View()))
		b.WriteString(" ")
		
		if m.currentStep < len(m.stepMessages) {
			b.WriteString(m.stepMessages[m.currentStep])
		}
		b.WriteString("\n\n")
		
		// プログレスバー
		progressValue := float64(m.currentStep) / float64(m.maxSteps)
		progressView := m.progress.ViewAs(progressValue)
		
		progressStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1).
			MarginTop(1)
		
		b.WriteString(progressStyle.Render(progressView))
		b.WriteString("\n\n")
		
		// ステップ表示
		for i, stepMsg := range m.stepMessages {
			var stepStyle lipgloss.Style
			if i < m.currentStep {
				stepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82")) // 完了 - 緑
				b.WriteString("✅ ")
			} else if i == m.currentStep {
				stepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("220")) // 進行中 - 黄
				b.WriteString("⏳ ")
			} else {
				stepStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // 未実行 - グレー
				b.WriteString("⭕ ")
			}
			
			b.WriteString(stepStyle.Render(stepMsg))
			b.WriteString("\n")
		}
		
	} else {
		// 成功表示
		successBoxStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("82")).
			Border(lipgloss.DoubleBorder()).
			Padding(1, 2).
			MarginTop(1).
			Bold(true)
		
		successContent := []string{
			"🎉 接続が完了しました！",
			"",
			"✅ トークンの検証に成功",
			"✅ ポートの取得に成功", 
			"✅ ポートの解放に成功",
			"",
			fmt.Sprintf("⏰ %d秒後にメイン画面に戻ります...", 5-m.successTimer/10),
		}
		
		b.WriteString(successBoxStyle.Render(strings.Join(successContent, "\n")))
	}

	// フッター（操作ヘルプ）
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		MarginTop(2).
		Italic(true)
	
	helpText := "ESC: メイン画面に戻る  •  Ctrl+C: 終了"
	
	b.WriteString("\n")
	b.WriteString(helpStyle.Render(helpText))

	return b.String()
}

// ReadTokenFromFile はトークンファイルから内容を読み取ります
func readTokenFromFile() (string, error) {
	filePath := "token"
	// ファイルが存在するか確認
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("ファイルが存在しません: %s", filePath)
		return "", err
	}

	// ファイルを読み取る
	data, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("ファイルの読み取りに失敗しました: %v", err)
		return "", err
	}

	// ファイルの内容を文字列として返す
	return string(data), nil
}
