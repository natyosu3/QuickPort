package screens

import (
	"QuickPort/share"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
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

// アニメーション用メッセージ
type tickWelcomeMsg time.Time
type pulseMsg struct{}

// アニメーション用コマンド
func doTickWelcome() tea.Cmd {
	return tea.Tick(time.Millisecond*200, func(t time.Time) tea.Msg {
		return tickWelcomeMsg(t)
	})
}

func doPulse() tea.Cmd {
	return tea.Tick(time.Second*2, func(t time.Time) tea.Msg {
		return pulseMsg{}
	})
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
	expireAt  string
}

// GitHubリリース情報構造体
type GitHubRelease struct {
	TagName string `json:"tag_name"`
	Body    string `json:"body"`
}

// メインメニューの Model
type WelcomeScreen struct {
	focusIndex            int
	serverActive          bool // サーバのアクティブ状態
	runtimeUpdateInterval time.Duration
	toggleInterval        time.Duration
	serverStatusChan      chan ServerStatusChan
	accountStatus         AccountStatus
	spinner               spinner.Model
	tickCount             int
	pulseState            bool
	showBanner            bool
	bannerOffset          int
	releaseMessage        string // GitHubリリースメッセージ
}

func NewWelcomeScreen() WelcomeScreen {
	accountStatus := getAccountStatus()
	releaseMessage := getReleaseMessage()
	
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	
	return WelcomeScreen{
		focusIndex:            0,
		serverActive:          true,        // 初期状態はアクティブ
		toggleInterval:        time.Second, // 状態を切り替える間隔
		runtimeUpdateInterval: time.Minute,
		serverStatusChan:      make(chan ServerStatusChan),
		accountStatus:         accountStatus,
		spinner:               s,
		tickCount:             0,
		pulseState:            false,
		showBanner:            true,
		bannerOffset:          0,
		releaseMessage:        releaseMessage,
	}
}

func (m WelcomeScreen) Init() tea.Cmd {
	// 複数のコマンドを同時に開始
	return tea.Batch(
		tea.Tick(m.runtimeUpdateInterval, func(t time.Time) tea.Msg {
			return "runtime_update"
		}),
		tea.Tick(m.toggleInterval, func(t time.Time) tea.Msg {
			return "toggle"
		}),
		m.spinner.Tick,
		doTickWelcome(),
		doPulse(),
	)
}

func (m WelcomeScreen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tickWelcomeMsg:
		m.tickCount++
		m.bannerOffset = (m.bannerOffset + 1) % 20
		return m, doTickWelcome()
	
	case pulseMsg:
		m.pulseState = !m.pulseState
		return m, doPulse()
	
	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

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
	
	return m, tea.Batch(cmds...)
}

// ランタイムアップデートのための関数
func updateRuntimeStatus(m *WelcomeScreen) tea.Cmd {
	m.serverActive = checkServerStatus()
	// リリースメッセージも更新
	m.releaseMessage = getReleaseMessage()
	return nil
}

// タイトル用のスタイル
var titleStyle = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	Align(lipgloss.Center).
	Padding(1).
	Width(116).                      // 幅を少し縮小
	Bold(true).                      // 太字に設定
	Foreground(lipgloss.Color("51")) // より鮮やかな青色

// グラデーション風のバナー
func createBanner(offset int, pulseState bool) string {
	banner := "✨ QuickPort - Fast & Secure Port Forwarding ✨"
	if pulseState {
		banner = "🌟 QuickPort - Fast & Secure Port Forwarding 🌟"
	}
	
	// 文字を動かすアニメーション
	chars := []rune(banner)
	for i := range chars {
		if (i+offset)%4 == 0 {
			chars[i] = []rune(strings.ToUpper(string(chars[i])))[0]
		}
	}
	return string(chars)
}

func (m WelcomeScreen) View() string {
	// アニメーションバナー
	bannerText := createBanner(m.bannerOffset, m.pulseState)
	banner := lipgloss.NewStyle().
		Foreground(lipgloss.Color("205")).
		Background(lipgloss.Color("235")).
		Padding(0, 2).
		Bold(true).
		Align(lipgloss.Center).
		Width(116).
		Render(bannerText)

	// タイトル
	title := titleStyle.Render("Welcome to QuickPort")

	// 左側のメニュー - 改善された見た目
	menuItems := []string{
		"🆕 アカウント作成",
		"🔑 トークン生成", 
		"🚀 ポート公開",
	}

	var leftView strings.Builder
	
	// メニューヘッダー
	menuHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("237")).
		Padding(0, 1).
		Bold(true).
		Width(50)
	
	leftView.WriteString(menuHeaderStyle.Render("📋 操作メニュー"))
	leftView.WriteString("\n\n")
	
	for i, item := range menuItems {
		var itemStyle lipgloss.Style
		prefix := fmt.Sprintf("[%d] ", i+1)
		
		if i == m.focusIndex {
			// フォーカスされたアイテム
			itemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("0")).
				Background(lipgloss.Color("205")).
				Padding(0, 1).
				Bold(true).
				Width(48)
			leftView.WriteString("→ ")
		} else {
			// 通常のアイテム
			itemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("240")).
				Width(48)
			leftView.WriteString("  ")
		}
		
		leftView.WriteString(itemStyle.Render(prefix + item))
		leftView.WriteString("\n")
	}
	
	// 終了オプション
	leftView.WriteString("\n")
	quitStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("160")).
		Italic(true)
	leftView.WriteString(quitStyle.Render("  [q] 終了"))

	// 右側のステータス - より詳細に
	var statusIcon, statusText string
	var statusStyle lipgloss.Style
	
	if m.serverActive {
		statusIcon = "🟢"
		statusText = "オンライン"
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
	} else {
		statusIcon = "🔴"
		statusText = "オフライン"
		statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("160"))
	}
	
	serverStatusHeader := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("237")).
		Padding(0, 1).
		Bold(true).
		Width(50).
		Render("🌐 サーバーステータス")
	
	rightView := serverStatusHeader + "\n\n"
	rightView += fmt.Sprintf("  %s %s %s\n", statusIcon, statusStyle.Render(statusText), m.spinner.View())
	rightView += "\n"
	
	// 接続統計（リリースメッセージ）
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("14")).
		Border(lipgloss.RoundedBorder()).
		Padding(1).
		Width(48)
	
	var displayMessage string
	if m.releaseMessage != "" {
		displayMessage = "📢 最新情報\n" + m.releaseMessage
	}
	
	rightView += statsStyle.Render(displayMessage)

	// アカウントステータスの表示 - 改善
	accountHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("237")).
		Padding(0, 1).
		Bold(true).
		Width(116).
		Align(lipgloss.Center)
	
	accountHeader := accountHeaderStyle.Render("👤 アカウント情報")
	
	accountContentStyle := lipgloss.NewStyle().
		Width(116).
		Padding(1, 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("39"))
	
	accountContent := fmt.Sprintf(
		"ユーザー名: %s  |  プラン: %s  |  帯域幅: %s  |  有効期限: %s",
		lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true).Render(m.accountStatus.username),
		lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(m.accountStatus.plan),
		lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true).Render(m.accountStatus.bandwidth),
		lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Render(m.accountStatus.expireAt),
	)
	
	accountStatus := lipgloss.JoinVertical(lipgloss.Center, accountHeader, accountContentStyle.Render(accountContent))

	// 現在の接続情報 - 改善
	connectionHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39")).
		Background(lipgloss.Color("237")).
		Padding(0, 1).
		Bold(true).
		Width(116).
		Align(lipgloss.Center)
	
	connectionHeader := connectionHeaderStyle.Render("🔗 接続情報")
	
	var connectionContent string
	if share.IsConnection {
		connectionBoxStyle := lipgloss.NewStyle().
			Width(116).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("82"))
		
		connectionContent = fmt.Sprintf(
			"🟢 接続中\n"+
			"公開IP: %s\n解放中ポート: %s",
			lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true).Render(share.PublicAddr),
			lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true).Render(share.Route),
		)
		connectionContent = connectionBoxStyle.Render(connectionContent)
	} else {
		connectionBoxStyle := lipgloss.NewStyle().
			Width(116).
			Padding(1, 2).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("240"))
		
		connectionContent = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(
			"🔴 未接続\n" +
			"公開IP: 未接続  |  解放中ポート: 未接続",
		)
		connectionContent = connectionBoxStyle.Render(connectionContent)
	}
	
	nowConnect := lipgloss.JoinVertical(lipgloss.Center, connectionHeader, connectionContent)

	// メインコンテンツ（左右結合）
	content := lipgloss.JoinHorizontal(
		lipgloss.Top, 
		lipgloss.NewStyle().Width(55).Padding(1).Render(leftView.String()), 
		lipgloss.NewStyle().Width(55).Padding(1).Render(rightView),
	)

	// フッター（ヘルプ）
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Align(lipgloss.Center).
		Width(116).
		Italic(true)
	
	help := helpStyle.Render("↑↓: 選択  •  Enter/Space: 実行  •  1-3: 直接選択  •  q: 終了")

	// すべてを結合
	return lipgloss.JoinVertical(
		lipgloss.Center, 
		banner,
		"",
		title, 
		"",
		accountStatus, 
		"",
		nowConnect, 
		"",
		content,
		"",
		help,
	)
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

// GitHubリリースメッセージを取得する関数
func getReleaseMessage() string {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	resp, err := client.Get("https://api.github.com/repos/natyosu3/QuickPort/releases/latest")
	if err != nil {
		log.Printf("GitHubリリース情報の取得に失敗しました: %v", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("GitHubリリース情報の取得に失敗しました (ステータス: %d)", resp.StatusCode)
		return ""
	}

	var release GitHubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		log.Printf("GitHubリリース情報のデコードに失敗しました: %v", err)
		return ""
	}

	// [メッセージ] xxxxx の部分を抽出
	re := regexp.MustCompile(`\[メッセージ\]\s*(.+)`)
	matches := re.FindStringSubmatch(release.Body)
	if len(matches) > 1 {
		return "  " + strings.TrimSpace(matches[1])
	}

	// [メッセージ]が見つからない場合は、bodyの最初の数行を返す
	lines := strings.Split(release.Body, "\n")
	if len(lines) > 0 && strings.TrimSpace(lines[0]) != "" {
		return "  " + strings.TrimSpace(lines[0])
	}

	return ""
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
