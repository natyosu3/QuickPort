package screens

import (
	"QuickPort/share"
	"net/http"
	"net/url"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	focusedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	blurredStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	activeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // 緑色
	inactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))  // 赤色
	cursorStyle   = focusedStyle
	noStyle       = lipgloss.NewStyle()

	// レイアウト用のスタイル
	leftColumnStyle  = lipgloss.NewStyle().Width(30).Padding(1)
	rightColumnStyle = lipgloss.NewStyle().Width(30).Padding(1).Align(lipgloss.Center)
)

// 画面切り替えメッセージ
type ScreenChangeMsg struct {
	Screen string
}

// 認証サーバの状態を取得するチャンネル用構造体
type ServerStatusChan struct {
	Status  string
	Message string
}

// アカウントステータス構造体
type AccountStatus struct {
	username  string
	plan      string
	bandwidth string
}

// 現在の接続情報を保持する構造体
type CurrentConnection struct {
	ClientID string
	IP       string
	Port     string
}

// メインメニューの Model
type WelcomeScreen struct {
	focusIndex        int
	serverActive      bool // サーバのアクティブ状態
	toggleInterval    time.Duration
	serverStatusChan  chan ServerStatusChan
	accountStatus     AccountStatus
	currentConnection CurrentConnection
}

func NewWelcomeScreen() WelcomeScreen {
	return WelcomeScreen{
		focusIndex:       0,
		serverActive:     true,        // 初期状態はアクティブ
		toggleInterval:   time.Second, // 状態を切り替える間隔
		serverStatusChan: make(chan ServerStatusChan),
		accountStatus: AccountStatus{
			username:  "ユーザー名", // ユーザー名の初期値
			plan:      "無料",    // プランの初期値
			bandwidth: "800KB", // 帯域幅の初期値
		},
		currentConnection: CurrentConnection{
			ClientID: "クライアントID",                    // クライアントIDの初期値
			IP:       "quickport.natyosu.com:54242", // 公開IPの初期値
			Port:     "解放中ポート",                      // 解放中ポートの初期値
		},
	}
}

func (m WelcomeScreen) Init() tea.Cmd {
	// 一定間隔で状態をトグルするコマンドを開始
	return tea.Tick(m.toggleInterval, func(t time.Time) tea.Msg {
		return "toggle"
	})
}

func (m WelcomeScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up":
			if m.focusIndex > 0 {
				m.focusIndex--
			}
		case "down":
			if m.focusIndex < 2 {
				m.focusIndex++
			}
		case "1":
			m.focusIndex = 0
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "create_account"}
			}
		case "2":
			m.focusIndex = 1
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "generate_token"}
			}
		case "3":
			m.focusIndex = 2
			if share.IsRunningFrpc {
				// frpcが起動している場合は、再度起動しないようにする
				return m, nil
			}
			return m, func() tea.Msg {
				return ScreenChangeMsg{Screen: "start_frpc"}
			}
		case "enter", " ":
			switch m.focusIndex {
			case 0:
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "create_account"}
				}
			case 1:
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "generate_token"}
				}
			case 2:
				if share.IsRunningFrpc {
					// frpcが起動している場合は、再度起動しないようにする
					return m, nil
				}
				return m, func() tea.Msg {
					return ScreenChangeMsg{Screen: "start_frpc"}
				}
			}
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	case string:
		if msg == "toggle" {
			// サーバの状態をトグル
			m.serverActive = !m.serverActive
			// 再度トグルコマンドを発行
			return m, tea.Tick(m.toggleInterval, func(t time.Time) tea.Msg {
				return "toggle"
			})
		}
	}

	updateRuntimeStatus(&m) // ランタイムアップデートのための関数を呼び出す

	return m, nil
}

// ランタイムアップデートのための関数
func updateRuntimeStatus(m *WelcomeScreen) tea.Cmd {
	// サーバの状態を確認するためのゴルーチンを開始
	go func() {
		ch := make(chan ServerStatusChan)
		go checkServerStatus(ch)

		select {
		case status := <-ch:
			if status.Status == "active" {
				m.serverActive = true
			} else if status.Status == "inactive" {
				m.serverActive = false
			}
		case <-time.After(5 * time.Second):
			m.serverActive = false // タイムアウト時は非アクティブとする
		}
	}()

	return nil
}

// タイトル用のスタイル
var titleStyle = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	Align(lipgloss.Center).
	Padding(1).
	Width(62).                       // 左右のビューの幅を合わせたサイズ
	Bold(true).                      // 太字に設定
	Foreground(lipgloss.Color("12")) // 青色に設定

func (m WelcomeScreen) View() string {
	// タイトル
	title := titleStyle.Render("Welcome to QuickPort")

	// 左側のメニュー
	menuItems := []string{
		"[1] アカウント作成",
		"[2] トークン生成",
		"[3] ポート公開",
	}

	var leftView string
	leftView += "[操作メニュー]\n"
	for i, item := range menuItems {
		if i == m.focusIndex {
			leftView += focusedStyle.Render(item) + "\n"
		} else {
			leftView += blurredStyle.Render(item) + "\n"
		}
	}
	leftView += "[q] 終了"

	// 右側のステータス
	statusIcon := activeStyle.Render("●")
	if !m.serverActive {
		statusIcon = inactiveStyle.Render("●")
	}
	rightView := statusIcon + " 認証サーバステータス"

	// アカウントステータスの表示
	accountStatus := lipgloss.NewStyle().
		Width(62).     // 全体の幅を揃える
		Padding(1, 2). // 上下左右にパディングを追加
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#ffffff")). // 黄色
		Render(
			"[アカウントステータス]\n" +
				"  ユーザー名: " + lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(m.accountStatus.username) + "\n" +
				"  プラン: " + lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.accountStatus.plan) + "\n" +
				"  帯域幅: " + lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.accountStatus.bandwidth),
		)

	// 現在の接続情報の表示
	nowConnect := lipgloss.NewStyle().
		Width(62).     // 全体の幅を揃える
		Padding(1, 2). // 上下左右にパディングを追加
		Align(lipgloss.Left).
		Foreground(lipgloss.Color("#ffffff")). // 黄色
		Render(
			"[現在の接続]\n" +
				"  クライアントID: " + lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render(m.currentConnection.ClientID) + "\n" +
				"  公開IP: " + lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(m.currentConnection.IP) + "\n" +
				"  解放中ポート: " + lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.currentConnection.Port),
		)

	// 左右を結合
	content := lipgloss.JoinHorizontal(lipgloss.Top, leftColumnStyle.Render(leftView), rightColumnStyle.Render(rightView))

	// タイトル、アカウントステータス、現在の接続情報、コンテンツを結合
	return lipgloss.JoinVertical(lipgloss.Top, title, accountStatus, nowConnect, content)
}

// 認証サーバがオンラインか確認する関数
func checkServerStatus(ch chan ServerStatusChan) {
	// pingエンドポイントにリクエストを送信
	parsedURL, err := url.Parse("https://quick-port-auth.natyosu.com/ping")
	if err != nil {
		ch <- ServerStatusChan{Status: "error", Message: "Invalid URL"}
		return
	}
	req := &http.Request{
		Method: "GET",
		URL:    parsedURL,
	}

	client := &http.Client{
		Timeout: 3 * time.Second, // タイムアウトを5秒に設定
	}

	resp, err := client.Do(req)
	if err != nil {
		ch <- ServerStatusChan{Status: "error", Message: "Request failed: " + err.Error()}
		return
	} else {
		if resp.StatusCode == http.StatusOK {
			ch <- ServerStatusChan{Status: "active", Message: "Server is active"}
		} else {
			ch <- ServerStatusChan{Status: "inactive", Message: "Server is inactive"}
		}
	}
	defer resp.Body.Close()

	close(ch)
}
