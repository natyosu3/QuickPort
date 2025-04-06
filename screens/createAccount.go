package screens

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	cAFocusedStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	cABlurredStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	cACursorStyle         = cAFocusedStyle
	cANoStyle             = lipgloss.NewStyle()
	cAHelpStyle           = cABlurredStyle
	cACursorModeHelpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	cAFocusedButton = cAFocusedStyle.Render("[ 登録 ]")
	cABlurredButton = fmt.Sprintf("[ %s ]", cABlurredStyle.Render("登録"))
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

// 情報をファイルに書き出す関数
func saveToFile(email, password string) error {
	file, err := os.OpenFile("accounts.txt", os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer file.Close()

	_, err = file.WriteString(fmt.Sprintf("メールアドレス: %s, パスワード: %s\n", email, password))
	return err
}

func InitialCreateAccountModel() CreateAccountModel {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
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

		switch i {
		case 0:
			t.Placeholder = "メールアドレス"
			t.Focus()
			t.PromptStyle = cAFocusedStyle
			t.TextStyle = cAFocusedStyle
			t.CharLimit = 64
		case 1:
			t.Placeholder = "パスワード"
			t.EchoMode = textinput.EchoPassword
			t.EchoCharacter = '•'
			t.CharLimit = 64
		case 2:
			t.Placeholder = "パスワード【確認】"
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
		case "ctrl+c", "esc":
			return m, tea.Quit

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
				if err := saveToFile(email, password); err != nil {
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
	title := titleStyle.Render("アカウント作成")
	b.WriteString(title)
	b.WriteString("\n\n") // タイトルとフォームの間にスペースを追加

	if m.loadding {
		b.WriteString(m.spinner.View())
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render(" アカウント作成中.\n"))
		return b.String()
	} else if m.isComp {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render("🎉アカウント作成完了"))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("222")).Render("➤ Enterキーでトークン発行に移動"))
		return b.String()
	}

	for i := range m.inputs {
		b.WriteString(m.inputs[i].View())
		if i < len(m.inputs)-1 {
			b.WriteRune('\n')
		}
	}

	button := &cABlurredButton
	if m.focusIndex == len(m.inputs) {
		button = &cAFocusedButton
	}
	fmt.Fprintf(&b, "\n\n%s\n\n", *button)

	// エラーメッセージを表示
	if m.errorMessage != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("160")).Render(m.errorMessage))
		b.WriteString("\n\n")
	}

	// ヘルプメッセージを追加
	helpMessage := cAHelpStyle.Render(
		"不具合や不明点はdiscordサーバか開発者個人へ連絡してください\n" +
			"discord server: https://discord.gg/3bsrZ4aBXK\n" +
			"開発者discord ID: natyosu.zip",
	)
	b.WriteString(helpMessage)

	return b.String()
}

func sendCreateAccountRequest(body []byte, ch chan accountChan) {
	// HTTPSリクエストを送信
	endpoint := "https://quick-port-auth.natyosu.com/auth/signup"
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
