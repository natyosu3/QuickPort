package screens

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	gTCursorModeHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	gTTokenStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("111")).Bold(true).Border(lipgloss.DoubleBorder())

	gTFocusedButton = gTFocusedStyle.Render("[ トークン発行 ]")
	gTBlurredButton = fmt.Sprintf("[ %s ]", gTBlurredStyle.Render("トークン発行"))
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
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	m := GenerateTokenModel{
		inputs:  make([]textinput.Model, 3),
		spinner: s,
		ch:      make(chan tokenChan),
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = gTCursorStyle
		t.CharLimit = 32

		switch i {
		case 0:
			t.Placeholder = "メールアドレス"
			t.Focus()
			t.PromptStyle = gTFocusedStyle
			t.TextStyle = gTFocusedStyle
			t.CharLimit = 64
		case 1:
			t.Placeholder = "パスワード"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		case 2:
			t.Placeholder = "マインクラフトサーバの公開ポート番号 ex:25565"
			t.CharLimit = 10
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

				localPort, err := strconv.Atoi(localPortStr)
				if err != nil {
					m.errorMessage = "ポート番号は数値で入力してください"
					return m, nil
				}

				var reqest Request
				reqest.RequestUserInfo.Email = email
				reqest.RequestUserInfo.Password = password
				reqest.RequestTokenMetadata.LocalPort = localPort
				reqest.RequestTokenMetadata.LocalIP = "127.0.0.1"
				reqest.RequestTokenMetadata.ProtocolType = "tcp"

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
	// endpoint := "https://quick-port-auth.natyosu.com/auth/token-issuance"
	endpoint := "http://163.44.96.225:8081/auth/token-issuance"
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
	title := titleStyle.Render("トークン発行")
	b.WriteString(title)
	b.WriteString("\n\n") // タイトルとフォームの間にスペースを追加

	if m.loadding {
		b.WriteString(m.spinner.View())
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render(" トークン発行中です.\n"))
		return b.String()
	} else if m.token != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render("🎉トークン発行完了"))
		b.WriteString("\n\n")
		b.WriteString(gTTokenStyle.Render(maskToken(m.token)))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render("戻る"))
		return b.String()
	}

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		if i < len(m.inputs)-1 {
			b.WriteRune('\n')
		}
	}

	button := &gTBlurredButton
	if m.focusIndex == len(m.inputs) {
		button = &gTFocusedButton
	}
	fmt.Fprintf(&b, "\n\n%s\n\n", *button)

	// エラーメッセージを表示
	if m.errorMessage != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("160")).Render(m.errorMessage))
		b.WriteString("\n\n")
	}

	// ヘルプメッセージを追加
	helpMessage := gTHelpStyle.Render(
		"不具合や不明点はdiscordサーバか開発者個人へ連絡してください\n" +
			"discord server: https://discord.gg/VgqaneJmaR\n" +
			"開発者discord ID: natyosu.zip",
	)
	b.WriteString(helpMessage)

	return b.String()
}

func maskToken(token string) string {
	if len(token) < 15 {
		// トークンが15文字未満の場合はそのまま返す
		return token
	}

	// トークンの先頭15文字を取得
	prefix := token[:5]              // 最初の5文字
	masked := "xxxxxxxxxxxxxxxxxxxx" // 後半20文字を'x'に置き換え

	// マスクされたトークンを返す
	return prefix + masked
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
