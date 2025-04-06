package screens

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"QuickPort/internal/frpc"
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
	focusIndex     int
	errorMessage   string
	isComp         bool
	ch             chan frpc.TokenAuthorizationChannel
	spinner        spinner.Model
	loadding       bool
	token          string
	tokenInfo      frpc.TokenAuthorizationChannel
	getPortLoading bool
	getPortCh      chan getPortChan
	remotePort     int
	getPortIsComp  bool
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

func InitialStartFrpcModel() StartFrpcModel {
	s := spinner.New()
	s.Spinner = spinner.Points
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("222"))
	m := StartFrpcModel{
		spinner:        s,
		ch:             make(chan frpc.TokenAuthorizationChannel),
		isComp:         false,
		getPortCh:      make(chan getPortChan),
		getPortLoading: false,
	}

	// トークンファイルからトークンを読み取る
	token, err := readTokenFromFile()
	if err != nil {
		log.Printf("トークンの読み取りに失敗しました: %v", err)
		m.errorMessage = "トークンの読み取りに失敗しました"
	}
	m.token = token

	// tokenが空でない場合は、トークンを検証する
	if m.token != "" {
		m.loadding = true
		go frpc.SendTokenValidityRequest(m.token, m.ch)
	}

	return m
}

func (m StartFrpcModel) Init() tea.Cmd {
	return m.spinner.Tick
}

func (m StartFrpcModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			return m, tea.Quit

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

	if m.loadding {
		select {
		case result := <-m.ch:
			// トークンの検証結果を受け取る
			if result.Status == "OK" {
				m.loadding = false
				m.isComp = true
				m.tokenInfo = result
				m.errorMessage = result.Message
			} else {
				m.loadding = false
				m.errorMessage = result.Message
			}
		default:
			// チャネルが空の場合は何もしない
		}
	}

	if m.isComp {
		m.errorMessage = "トークンの検証に成功しました"

		if !m.getPortIsComp {
			// トークンの検証に成功した場合、ポートを取得する
			if !m.getPortLoading {
				m.getPortLoading = true
				go getRemotePort(m.token, m.getPortCh)
			}

			select {
			case result := <-m.getPortCh:
				// ポート取得結果を受け取る
				if result.Status == "OK" {
					m.getPortLoading = false
					m.getPortIsComp = true
					m.errorMessage = result.Message
					m.remotePort = result.Port
					go frpc.StartFrpc(m.tokenInfo.TokenInfo, m.remotePort)
				} else {
					m.getPortLoading = false
					m.errorMessage = result.Message
				}
			default:
				// チャネルが空の場合は何もしない
			}
		}
	}

	// Handle character input and blinking
	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *StartFrpcModel) updateInputs(msg tea.Msg) tea.Cmd {
	// スピナーの更新コマンドを取得
	var spinnerCmd tea.Cmd
	m.spinner, spinnerCmd = m.spinner.Update(msg)

	return spinnerCmd
}

func (m StartFrpcModel) View() string {
	var b strings.Builder

	if m.token != "" && !m.getPortIsComp {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("トークン: " + m.token))
		b.WriteString("\n\n")
	}

	if m.loadding && !m.getPortIsComp {
		b.WriteString(m.spinner.View() + " トークンの検証中...\n\n")
	}

	if m.getPortLoading && !m.getPortIsComp {
		b.WriteString(m.spinner.View() + " ポート取得中...\n\n")
	}

	if m.getPortIsComp {
		b.WriteString(m.spinner.View() + " ポート取得完了\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render(fmt.Sprintf("ポート: %d", m.remotePort)))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("トークン: " + m.token))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("トークンの検証に成功しました"))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("ポートの取得に成功しました"))
		b.WriteString("\n\n")
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("次のステップに進むにはEnterを押してください"))
	}

	// エラーメッセージを表示
	if m.errorMessage != "" && !m.getPortIsComp {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("160")).Render(m.errorMessage))
		b.WriteString("\n\n")
	}

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

func getRemotePort(token string, ch chan getPortChan) {
	defer close(ch) // 関数終了時に一度だけチャンネルを閉じる

	// HTTPSリクエストを送信
	endpoint := "https://vps-manager-api.natyosu.com/free_port_acquisition"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		log.Printf("HTTPリクエストの作成に失敗しました: %v", err)
		ch <- getPortChan{
			Status:  "ERROR",
			Message: "HTTPリクエストの作成に失敗しました: " + err.Error(),
			Port:    -1,
		}
		return
	}
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("HTTPリクエストの送信に失敗しました: %v", err)
		ch <- getPortChan{
			Status:  "ERROR",
			Message: "HTTPリクエストの送信に失敗しました: " + err.Error(),
			Port:    -1,
		}
		return
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取る
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("レスポンスボディの読み取りに失敗しました: %v", err)
		ch <- getPortChan{
			Status:  "ERROR",
			Message: "レスポンスボディの読み取りに失敗しました: " + err.Error(),
			Port:    -1,
		}
		return
	}

	// レスポンスボディをパース
	var parsedResponse struct {
		Message string `json:"message"`
		Status  string `json:"status"`
		Port    int    `json:"port"`
	}
	err = json.Unmarshal(respBody, &parsedResponse)
	if err != nil {
		log.Printf("286: レスポンスボディのパースに失敗しました: %v", err)
		ch <- getPortChan{
			Status:  "ERROR",
			Message: "レスポンスボディのパースに失敗しました: " + err.Error(),
			Port:    -1,
		}
		return
	}

	if parsedResponse.Status == "ERROR" {
		ch <- getPortChan{
			Status:  "ERROR",
			Message: parsedResponse.Message,
			Port:    -1,
		}
	} else {
		ch <- getPortChan{
			Status:  "OK",
			Message: parsedResponse.Message,
			Port:    parsedResponse.Port,
		}
	}
}
