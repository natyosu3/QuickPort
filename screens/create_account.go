package screens

import (
	"QuickPort/share"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/ini.v1"
)

var (
	cAFocusedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	cACursorStyle         = cAFocusedStyle
	cANoStyle             = lipgloss.NewStyle()
	cATitleStyle = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		Align(lipgloss.Center).
		Padding(1).
		Width(60).
		Bold(true).
		Foreground(lipgloss.Color("205"))
	cAFocusedButton = lipgloss.NewStyle().
		Foreground(lipgloss.Color("15")).
		Background(lipgloss.Color("205")).
		Bold(true).
		Padding(0, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("205")).
		Render("アカウント登録")
	cABlurredButton = lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Background(lipgloss.Color("236")).
		Padding(0, 3).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Render("アカウント登録")
)

type CreateAccountModel struct {
	focusIndex   int
	inputs       []textinput.Model
	cursorMode   cursor.Mode
	errorMessage string
	isComp       bool
	ch           chan accountChan
	spinner      spinner.Model
	loadding     bool
}

type createAccountRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type accountChan struct {
	status  string
	message string
}

// パスワードのバリデーション関数
func validatePassword(password, confirmPassword string) error {
	if len(password) < 5 {
		return errors.New("パスワードは5文字以上である必要があります")
	}

	// 確認用パスワードと一致しているか
	if password != confirmPassword {
		return errors.New("パスワードが一致しません")
	}

	return nil
}

// 情報をiniファイルに書き出す関数
func saveToFile(email string) error {
	// 新しいiniファイルを作成
	cfg := ini.Empty()

	// セクションとキーを設定
	section, err := cfg.NewSection("Account")
	if err != nil {
		return err
	}
	_, err = section.NewKey("Email", email)
	if err != nil {
		return err
	}

	// ファイルに保存（上書き）
	err = cfg.SaveTo("accounts.ini")
	if err != nil {
		return err
	}

	return nil
}

func InitialCreateAccountModel() CreateAccountModel {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	m := CreateAccountModel{
		inputs:  make([]textinput.Model, 3),
		spinner: s,
		ch:      make(chan accountChan),
		isComp:  false,
	}

	var t textinput.Model
	for i := range m.inputs {
		t = textinput.New()
		t.Cursor.Style = cACursorStyle
		t.CharLimit = 32
		t.Width = 40

		switch i {
		case 0:
			t.Placeholder = "example@domain.com"
			t.Focus()
			t.PromptStyle = cAFocusedStyle
			t.TextStyle = cAFocusedStyle
			t.CharLimit = 64
		case 1:
			t.Placeholder = "5文字以上のパスワード"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		case 2:
			t.Placeholder = "パスワードを再入力"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		}

		m.inputs[i] = t
	}

	return m
}

func (m CreateAccountModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, textinput.Blink)
}

func (m CreateAccountModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			if s == "enter" && m.isComp {
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "generate_token"}
				}
			}
			// Did the user press enter while the submit button was focused?
			// If so, exit.
			if s == "enter" && m.focusIndex == len(m.inputs) {
				// 登録ボタンが押された場合
				email := m.inputs[0].Value()
				password := m.inputs[1].Value()
				confirmPassword := m.inputs[2].Value()

				// パスワードのバリデーション
				if err := validatePassword(password, confirmPassword); err != nil {
					m.errorMessage = err.Error() // エラーメッセージを設定
					return m, nil
				}

				req := createAccountRequest{
					Email:    email,
					Password: confirmPassword,
				}

				// リクエストボディをJSONに変換
				requestBody, err := json.Marshal(req)
				if err != nil {
					log.Printf("リクエストボディの作成に失敗しました: %v", err)
					m.errorMessage = "リクエストの作成に失敗しました"
					return m, nil
				}

				m.loadding = true
				go sendCreateAccountRequest(requestBody, m.ch)

				// ファイルに保存
				if err := saveToFile(email); err != nil {
					fmt.Println("ファイル保存エラー:", err)
					return InitialCreateAccountModel(), nil
				}
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
					m.inputs[i].PromptStyle = cAFocusedStyle
					m.inputs[i].TextStyle = cAFocusedStyle
					continue
				}
				// Remove focused state
				m.inputs[i].Blur()
				m.inputs[i].PromptStyle = cANoStyle
				m.inputs[i].TextStyle = cANoStyle
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
				m.isComp = true
			}
		default:
			// チャンネルがまだ閉じられていない場合は何もしない
		}
	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *CreateAccountModel) updateInputs(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, len(m.inputs))

	// スピナーの更新コマンドを取得
	var spinnerCmd tea.Cmd
	m.spinner, spinnerCmd = m.spinner.Update(msg)

	// Only text inputs with Focus() set will respond, so it's safe to simply
	// update all of them here without any further logic.
	for i := range m.inputs {
		m.inputs[i], cmds[i] = m.inputs[i].Update(msg)
	}

	return tea.Batch(append(cmds, spinnerCmd)...)
}

func (m CreateAccountModel) View() string {
	var b strings.Builder

	// タイトルを追加
	title := cATitleStyle.Render("アカウント作成")
	b.WriteString(title)
	b.WriteString("\n\n")

	if m.loadding {
		loadingStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)
		
		b.WriteString(lipgloss.NewStyle().Align(lipgloss.Center).Render(
			m.spinner.View() + " " + loadingStyle.Render("アカウント作成中..."),
		))
		b.WriteString("\n\n")
		return b.String()
	} else if m.isComp {
		successStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("46")).
			Bold(true)
		
		instructionStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205"))
		
		b.WriteString(successStyle.Render("🎉 アカウント作成完了"))
		b.WriteString("\n\n")
		b.WriteString(instructionStyle.Render("➤ Enterキーでトークン発行に移動"))
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
	labels := []string{"メールアドレス", "パスワード", "パスワード確認"}
	
	for i := range m.inputs {
		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true).
			MarginBottom(1)
		
		formContent.WriteString(labelStyle.Render(labels[i]))
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
		button = cAFocusedButton
	} else {
		button = cABlurredButton
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

func sendCreateAccountRequest(body []byte, ch chan accountChan) {
	// HTTPSリクエストを送信
	endpoint := share.BASE_API_URL + "/auth/signup"
	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(body))
	if err != nil {
		log.Printf("HTTPリクエストの作成に失敗しました: %v", err)
		ch <- accountChan{
			status:  "ERROR",
			message: "HTTPリクエストの作成に失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("HTTPリクエストの送信に失敗しました: %v", err)
		ch <- accountChan{
			status:  "ERROR",
			message: "HTTPリクエストの送信に失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取る
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("レスポンスボディの読み取りに失敗しました: %v", err)
		ch <- accountChan{
			status:  "ERROR",
			message: "レスポンスボディの読み取りに失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}

	// レスポンスボディをパース
	var parsedResponse struct {
		Message string `json:"message"`
		Status  string `json:"status"`
	}
	err = json.Unmarshal(respBody, &parsedResponse)
	if err != nil {
		log.Printf("347 レスポンスボディのパースに失敗しました: %v", err)
		ch <- accountChan{
			status:  "ERROR",
			message: "347 レスポンスボディのパースに失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}

	// パース結果をログに出力
	log.Printf("レスポンス: message=%s, status=%s", parsedResponse.Message, parsedResponse.Status)

	if parsedResponse.Status == "ERROR" {
		ch <- accountChan{
			status:  "ERROR",
			message: parsedResponse.Message,
		}
	} else {
		ch <- accountChan{
			status:  "OK",
			message: parsedResponse.Message,
		}
	}
	close(ch) // チャンネルを閉じる
}
