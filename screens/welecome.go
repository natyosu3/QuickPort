package screens

import (
	"QuickPort/share"
	"log"
	"net/http"
	"net/url"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/ini.v1"
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

// アカウント情報更新メッセージ
type UpdateAccountStatusMsg struct{}

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
	expireAt  string
}

// メインメニューの Model
type WelcomeScreen struct {
	focusIndex            int
	serverActive          bool // サーバのアクティブ状態
	runtimeUpdateInterval time.Duration
	toggleInterval        time.Duration
	serverStatusChan      chan ServerStatusChan
	accountStatus         AccountStatus
}

func NewWelcomeScreen() WelcomeScreen {
	accountStatus := getAccountStatus()
	return WelcomeScreen{
		focusIndex:            0,
		serverActive:          true,        // 初期状態はアクティブ
		toggleInterval:        time.Second, // 状態を切り替える間隔
		runtimeUpdateInterval: time.Minute,
		serverStatusChan:      make(chan ServerStatusChan),
		accountStatus:         accountStatus,
	}
}

func (m WelcomeScreen) Init() tea.Cmd {
	// 1分ごとにランタイムアップデートを実行するコマンドを開始
	return tea.Batch(
		tea.Tick(m.runtimeUpdateInterval, func(t time.Time) tea.Msg {
			return "runtime_update"
		}),
		tea.Tick(m.toggleInterval, func(t time.Time) tea.Msg {
			return "toggle"
		}),
	)
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
		if msg == "runtime_update" {
			// 1分ごとにランタイムアップデートを実行
			updateRuntimeStatus(&m)
			// 再度ランタイムアップデートコマンドを発行
			return m, tea.Tick(m.runtimeUpdateInterval, func(t time.Time) tea.Msg {
				return "runtime_update"
			})
		}
	case UpdateAccountStatusMsg:
		// アカウント情報を更新
		m.accountStatus = getAccountStatus()
		return m, nil
	}
	return m, nil
}

// ランタイムアップデートのための関数
func updateRuntimeStatus(m *WelcomeScreen) tea.Cmd {

	m.serverActive = checkServerStatus()

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

	var statusIcon string
	// 右側のステータス
	if m.serverActive {
		statusIcon = activeStyle.Render("●")
	} else {
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
				"  帯域幅: " + lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(m.accountStatus.bandwidth) +
				"  有効期限: " + lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Render(m.accountStatus.expireAt),
		)

	var nowConnect string
	if share.IsConnection {
		// 現在の接続情報の表示
		nowConnect = lipgloss.NewStyle().
			Width(120).    // 全体の幅を揃える
			Padding(1, 2). // 上下左右にパディングを追加
			Align(lipgloss.Left).
			Foreground(lipgloss.Color("#ffffff")). // 黄色
			Render(
				"[現在の接続]\n" +
					"  クライアントID: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(1).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("xxxxxxxxxxxxxxxxxxxxxxxxxx"),
				) + "\n" +
					"  公開IP: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(9).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(share.PublicAddr),
				) + "\n" +
					"  解放中ポート: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(3).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render(share.Route),
				),
			)
	} else {
		// 現在の接続情報の表示
		nowConnect = lipgloss.NewStyle().
			Width(62).     // 全体の幅を揃える
			Padding(1, 2). // 上下左右にパディングを追加
			Align(lipgloss.Left).
			Foreground(lipgloss.Color("#ffffff")). // 黄色
			Render(
				"[現在の接続]\n" +
					"  クライアントID: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(1).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Render("未接続"),
				) + "\n" +
					"  公開IP: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(9).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render("未接続"),
				) + "\n" +
					"  解放中ポート: " + lipgloss.JoinHorizontal(lipgloss.Top,
					lipgloss.NewStyle().Width(3).Render(""),
					lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Render("未接続"),
				),
			)
	}

	// 左右を結合
	content := lipgloss.JoinHorizontal(lipgloss.Top, leftColumnStyle.Render(leftView), rightColumnStyle.Render(rightView))

	// タイトル、アカウントステータス、現在の接続情報、コンテンツを結合
	return lipgloss.JoinVertical(lipgloss.Top, title, accountStatus, nowConnect, content)
}

// 認証サーバがオンラインか確認する関数
func checkServerStatus() bool {
	// pingエンドポイントにリクエストを送信
	parsedURL, err := url.Parse("https://quick-port-auth.natyosu.com/ping")
	if err != nil {
		return false
	}
	req := &http.Request{
		Method: "GET",
		URL:    parsedURL,
	}

	client := &http.Client{
		Timeout: 5 * time.Second, // タイムアウトを5秒に設定
	}

	resp, err := client.Do(req)
	if err != nil {
		return false
	} else {
		if resp.StatusCode == http.StatusOK {
			return true
		} else {
			return false
		}
	}
}

// ユーザ情報を取得する関数
func getAccountStatus() AccountStatus {
	// iniファイルを読み込む
	cfg, err := ini.Load("accounts.ini")
	if err != nil {
		log.Printf("accounts.iniの読み込みに失敗しました: %v", err)
		return AccountStatus{
			username:  "アカウント情報が見つかりません",
			plan:      "不明",
			bandwidth: "不明",
			expireAt:  "不明",
		}
	}

	// セクション "Account" から情報を取得
	section := cfg.Section("Account")
	email := section.Key("Email").String()
	plan := section.Key("Plan").String()
	bandwidth := section.Key("Bandwidth").String()
	expireAt := section.Key("ExpireAt").String()

	// ユーザ名の表示形式を決定（Emailから生成）
	var displayUsername string
	if email != "" {
		if len(email) > 10 {
			displayUsername = email[0:5] + "..." + email[len(email)-5:]
		} else {
			displayUsername = email
		}
	} else {
		displayUsername = "アカウント情報が見つかりません"
	}

	// デフォルト値の設定
	if plan == "" {
		plan = "無料"
	}
	if bandwidth == "" {
		bandwidth = "800KB"
	}
	if expireAt == "" {
		expireAt = "未設定"
	} else {
		// 有効期限が設定されている場合は、フォーマットを整える
		// 2027-07-20T21:04:44+09:00 -> 2027年07月20日 21:04:44
		if parsedTime, err := time.Parse(time.RFC3339, expireAt); err == nil {
			expireAt = parsedTime.Format("2006年01月02日 15:04:05")
		} else {
			// パースに失敗した場合は元の文字列をそのまま使用
			log.Printf("有効期限の解析に失敗しました: %v, 元の値: %s", err, expireAt)
		}
	}

	return AccountStatus{
		username:  displayUsername,
		plan:      plan,
		bandwidth: bandwidth,
		expireAt:  expireAt,
	}
}
