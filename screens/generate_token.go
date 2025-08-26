package screens

import (
	"QuickPort/share"
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/ini.v1"
)

var (
	gTFocusedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	gTBlurredStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	gTCursorStyle         = gTFocusedStyle
	gTNoStyle             = lipgloss.NewStyle()
	gTHelpStyle           = gTBlurredStyle
	gTTokenStyle          = lipgloss.NewStyle().
		Foreground(lipgloss.Color("46")).
		Background(lipgloss.Color("22")).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("46")).
		Padding(1, 2).
		Align(lipgloss.Center)
	gTTitleStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		Align(lipgloss.Center).
		Padding(1).
		Width(60).
		Bold(true).
		Foreground(lipgloss.Color("205"))
	gTFocusedButton = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("205")).
		Bold(true).
		Padding(0, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render("トークン発行")
	gTBlurredButton = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("236")).
		Padding(0, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Render("トークン発行")
)

type RequestTokenMetadata struct {
	LocalIP      string `json:"local_ip"`
	LocalPort    int    `json:"local_port"`
	ProtocolType string `json:"protocol_type"`
}

type RequestUserInfo struct {
	Email    string `json:"email,omitempty"`
	Password string `json:"password"`
	UserName string `json:"user_name,omitempty"`
}

type Request struct {
	RequestTokenMetadata RequestTokenMetadata `json:"request_token_metadata"`
	RequestUserInfo      RequestUserInfo      `json:"request_user_info"`
}

type tokenChan struct {
	status  string
	message string
	token   string
}

type GenerateTokenModel struct {
	focusIndex   int
	inputs       []textinput.Model
	cursorMode   cursor.Mode
	errorMessage string
	token        string
	spinner      spinner.Model
	loadding     bool
	ch           chan tokenChan
}

func InitialGenerateTokenModel() GenerateTokenModel {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	m := GenerateTokenModel{
		inputs:  make([]textinput.Model, 4),
		spinner: s,
		ch:      make(chan tokenChan),
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = gTCursorStyle
		t.CharLimit = 32
		t.Width = 40

		switch i {
		case 0:
			t.Placeholder = "example@domain.com"
			t.Focus()
			t.PromptStyle = gTFocusedStyle
			t.TextStyle = gTFocusedStyle
			t.CharLimit = 64
		case 1:
			t.Placeholder = "パスワードを入力"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		case 2:
			t.Placeholder = "25565"
			t.CharLimit = 10
		case 3:
			t.Placeholder = "tcp または udp"
			t.CharLimit = 3
		}

		m.inputs[i] = t
	}

	return m
}

func (m GenerateTokenModel) Init() tea.Cmd {
	// スピナーの初期化コマンドを返す
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

func (m GenerateTokenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "welcome"}
			}

		// Change cursor mode
		case "ctrl+r":
			m.cursorMode++
			if m.cursorMode > cursor.CursorHide {
				m.cursorMode = cursor.CursorBlink
			}
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := range m.inputs {
				cmds[i] = m.inputs[i].Cursor.SetMode(m.cursorMode)
			}
			return m, tea.Batch(cmds...)

		// Set focus to next input
		case "tab", "shift+tab", "enter", "up", "down":
			s := msg.String()
			// トークン発行後エンターを押された場合, welcomeページに遷移
			if s == "enter" && m.token != "" {
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "welcome"}
				}
			}

			if s == "enter" && m.focusIndex == len(m.inputs) {
				// 登録ボタンが押された場合
				email := m.inputs[0].Value()
				password := m.inputs[1].Value()
				localPortStr := m.inputs[2].Value()
				protocolType := strings.ToLower(strings.TrimSpace(m.inputs[3].Value()))

				localPort, err := strconv.Atoi(localPortStr)
				if err != nil {
					m.errorMessage = "ポート番号は数値で入力してください"
					return m, nil
				}

				// プロトコルタイプの検証
				if protocolType != "tcp" && protocolType != "udp" {
					m.errorMessage = "プロトコルタイプは tcp または udp で入力してください"
					return m, nil
				}

				var reqest Request
				reqest.RequestUserInfo.Email = email
				reqest.RequestUserInfo.Password = password
				reqest.RequestTokenMetadata.LocalPort = localPort
				reqest.RequestTokenMetadata.LocalIP = "127.0.0.1"
				reqest.RequestTokenMetadata.ProtocolType = protocolType

				// リクエストボディをJSONに変換
				requestBody, err := json.Marshal(reqest)
				if err != nil {
					log.Printf("リクエストボディの作成に失敗しました: %v", err)
					m.errorMessage = "リクエストの作成に失敗しました"
					return m, nil
				}

				m.loadding = true
				go sendTokenRequest(requestBody, m.ch)

				return m, nil
			}

			// Cycle indexes
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > len(m.inputs) {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = len(m.inputs)
			}

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i <= len(m.inputs)-1; i++ {
				if i == m.focusIndex {
					// Set focused state
					cmds[i] = m.inputs[i].Focus()
					m.inputs[i].PromptStyle = gTFocusedStyle
					m.inputs[i].TextStyle = gTFocusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].Blur()
				m.inputs[i].PromptStyle = gTNoStyle
				m.inputs[i].TextStyle = gTNoStyle
			}

			return m, tea.Batch(cmds...)
		}
	}

	if m.loadding {
		select {
		case result := <-m.ch:
			m.loadding = false

			if result.status == "ERROR" {
				m.errorMessage = result.message
			} else {
				m.token = result.token
			}

		default:
			// チャンネルがまだ閉じられていない場合は何もしない
		}
	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)
	return m, cmd
}

func sendTokenRequest(body []byte, ch chan tokenChan) {
	// HTTPSリクエストを送信
	endpoint := share.BASE_API_URL + "/auth/token-issuance"
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("HTTPリクエストの作成に失敗しました: %v", err)
		ch <- tokenChan{
			status:  "ERROR",
			message: "HTTPリクエストの作成に失敗しました: " + err.Error(),
			token:   "",
		}
		close(ch)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("HTTPリクエストの送信に失敗しました: %v", err)
		ch <- tokenChan{
			status:  "ERROR",
			message: "HTTPリクエストの送信に失敗しました: " + err.Error(),
			token:   "",
		}
		close(ch)
		return
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取る
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("レスポンスボディの読み取りに失敗しました: %v", err)
		ch <- tokenChan{
			status:  "ERROR",
			message: "レスポンスボディの読み取りに失敗しました: " + err.Error(),
			token:   "",
		}
		close(ch)
		return
	}

	// レスポンスボディをパース
	var parsedResponse struct {
		Message   string `json:"message"`
		Status    string `json:"status"`
		Token     string `json:"token"`
		Email     string `json:"email,omitempty"`
		Plan      string `json:"plan,omitempty"`
		Bandwidth string `json:"bandwidth_limit,omitempty"`
		ExpireAt  string `json:"expire_at,omitempty"`
	}
	err = json.Unmarshal(respBody, &parsedResponse)
	if err != nil {
		log.Printf("レスポンスボディのパースに失敗しました: %v", err)
		ch <- tokenChan{
			status:  "ERROR",
			message: "レスポンスボディのパースに失敗しました: " + err.Error(),
			token:   "",
		}
		close(ch)
		return
	}

	// パース結果をログに出力
	log.Printf("レスポンス: message=%s, status=%s, token=%s", parsedResponse.Message, parsedResponse.Status, parsedResponse.Token)

	if parsedResponse.Status == "ERROR" {
		ch <- tokenChan{
			status:  "ERROR",
			message: parsedResponse.Message,
			token:   "",
		}
	} else {
		// トークンをファイルに書き出す
		if err := writeTokenToFile(parsedResponse.Token); err != nil {
			log.Printf("トークンのファイル書き出しに失敗しました: %v", err)
			ch <- tokenChan{
				status:  "ERROR",
				message: "トークンのファイル書き出しに失敗しました",
				token:   "",
			}
			return
		}

		// アカウント情報をaccounts.iniに保存
		if err := updateAccountInfo(parsedResponse.Email, parsedResponse.Plan, parsedResponse.Bandwidth, parsedResponse.ExpireAt); err != nil {
			log.Printf("アカウント情報の更新に失敗しました: %v", err)
			// アカウント情報の更新に失敗してもトークンは有効なので、エラーにはしない
		}

		ch <- tokenChan{
			status:  "OK",
			message: parsedResponse.Message,
			token:   parsedResponse.Token,
		}
	}
	close(ch) // チャンネルを閉じる
}

func writeTokenToFile(token string) error {
	file, err := os.OpenFile("token", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(token)
	return err
}

func (m *GenerateTokenModel) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	// スピナーの更新コマンドを取得
	var spinnerCmd tea.Cmd
	m.spinner, spinnerCmd = m.spinner.Update(msg)

	// テキスト入力の更新コマンドを取得
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	// スピナーの更新コマンドを含めて返す
	return tea.Batch(append(cmds, spinnerCmd)...)
}
func (m GenerateTokenModel) View() string {
	var b strings.Builder

	// タイトルを追加
	title := gTTitleStyle.Render("トークン発行")
	b.WriteString(title)
	b.WriteString("\n\n")

	if m.loadding {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)
		
		b.WriteString(lipgloss.NewStyle().Align(lipgloss.Center).Render(
			m.spinner.View() + " " + loadingStyle.Render("トークン発行中..."),
		))
		b.WriteString("\n\n")
		return b.String()
	} else if m.token != "" {
		successStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)
		
		instructionStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))
		
		b.WriteString(successStyle.Render("🎉 トークン発行完了"))
		b.WriteString("\n\n")
		
		tokenContainer := lipgloss.NewStyle().
			Align(lipgloss.Center).
			MarginTop(1).
			MarginBottom(2)
		
		b.WriteString(tokenContainer.Render(gTTokenStyle.Render("Token: " + maskToken(m.token))))
		b.WriteString("\n")
		b.WriteString(instructionStyle.Render("➤ Enterキーで戻る"))
		b.WriteString("\n\n")
		return b.String()
	}

	// フォームのレンダリング
	formStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1, 2).
		MarginBottom(1)

	var formContent strings.Builder
	
	// 入力フィールドのラベル
	labels := []string{"メールアドレス", "パスワード", "Minecraftサーバのポート番号", "プロトコルタイプ"}
	descriptions := []string{
		"アカウント作成時に使用したメールアドレス",
		"アカウント作成時に設定したパスワード", 
		"公開するMinecraftサーバのポート番号（例: 25565）",
		"使用するプロトコル（tcp または udp）",
	}
	
	for i := range m.inputs {
		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			MarginBottom(1)
		
		descStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true).
			MarginBottom(1)
		
		formContent.WriteString(labelStyle.Render(labels[i]))
		formContent.WriteString("\n")
		formContent.WriteString(descStyle.Render(descriptions[i]))
		formContent.WriteString("\n")
		formContent.WriteString(m.inputs[i].View())
		if i < len(m.inputs)-1 {
			formContent.WriteString("\n\n")
		}
	}

	b.WriteString(formStyle.Render(formContent.String()))
	b.WriteString("\n")

	// ボタンのレンダリング
	var button string
	if m.focusIndex == len(m.inputs) {
		button = gTFocusedButton
	} else {
		button = gTBlurredButton
	}
	
	buttonContainer := lipgloss.NewStyle().
		Align(lipgloss.Center).
		MarginTop(1).
		MarginBottom(1)
	
	b.WriteString(buttonContainer.Render(button))
	b.WriteString("\n")

	// エラーメッセージを表示
	if m.errorMessage != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("160")).
			Background(lipgloss.Color("52")).
			Padding(0, 1).
			Bold(true).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("160"))
		
		b.WriteString(errorStyle.Render("⚠ " + m.errorMessage))
		b.WriteString("\n\n")
	}

	// 重要な注意事項
	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("214")).
		Background(lipgloss.Color("58")).
		Bold(true).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(0, 1).
		MarginBottom(1)
	
	b.WriteString(warningStyle.Render("💡 発行されたトークンは安全に保管してください"))
	b.WriteString("\n\n")

	// 操作説明
	navigationStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Border(lipgloss.NormalBorder()).
		BorderTop(true).
		BorderForeground(lipgloss.Color("240")).
		PaddingTop(1).
		MarginTop(1)
	
	navigation := "操作方法: Tab/↑↓で移動 | Enter で実行 | Esc で戻る"
	b.WriteString(navigationStyle.Render(navigation))
	b.WriteString("\n\n")

	// ヘルプメッセージを追加
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Italic(true)
	
	helpMessage := helpStyle.Render(
		"不具合や不明点はdiscordサーバか開発者個人へ連絡してください\n" +
			"discord server: https://discord.gg/VgqaneJmaR\n" +
			"開発者discord ID: natyosu.zip",
	)
	b.WriteString(helpMessage)

	return b.String()
}

func maskToken(token string) string {
	if len(token) < 10 {
		// トークンが10文字未満の場合はそのまま返す
		return token
	}

	// トークンの先頭8文字と末尾4文字を表示し、中間をマスク
	if len(token) <= 16 {
		prefix := token[:4]
		suffix := token[len(token)-4:]
		masked := strings.Repeat("*", len(token)-8)
		return prefix + masked + suffix
	}

	// 長いトークンの場合
	prefix := token[:8]              // 最初の8文字
	suffix := token[len(token)-4:]   // 最後の4文字
	masked := strings.Repeat("*", 16) // 中間16文字を'*'に置き換え

	// マスクされたトークンを返す
	return prefix + masked + suffix
}

// updateAccountInfo は accounts.ini にアカウント情報を更新する
func updateAccountInfo(email, plan, bandwidth, expireAt string) error {
	// accounts.ini ファイルを読み込み、存在しない場合は新しく作成
	cfg, err := ini.Load("accounts.ini")
	if err != nil {
		// ファイルが存在しない場合は新しく作成
		cfg = ini.Empty()
	}

	// Account セクションを取得または作成
	section := cfg.Section("Account")
	
	// メールアドレス、プラン、帯域幅を設定（空でない場合のみ）
	if email != "" {
		section.Key("Email").SetValue(email)
	}
	if plan != "" {
		section.Key("Plan").SetValue(plan)
	}
	if bandwidth != "" {
		section.Key("Bandwidth").SetValue(bandwidth)
	}
	if expireAt != "" {
		section.Key("ExpireAt").SetValue(expireAt)
	}

	// ファイルに保存
	return cfg.SaveTo("accounts.ini")
}
