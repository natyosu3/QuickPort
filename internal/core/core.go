package core

import (
	"QuickPort/share"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"
)

// メッセージタイプ（サーバーと同じ）
const (
	MSG_TYPE_LOGIN    = "login"
	MSG_TYPE_NEW_CONN = "new_conn"
	MSG_TYPE_DATA     = "data"
	MSG_TYPE_CLOSE    = "close"
	MSG_TYPE_KICK     = "kick"
)

// メッセージ構造体
type Message struct {
	Type      string `json:"type"`
	ProxyName string `json:"proxy_name"`
	ConnID    string `json:"conn_id"`
	Data      []byte `json:"data,omitempty"`
	Token     string `json:"token,omitempty"`    // 新規: 認証トークン
	ErrorMsg  string `json:"error_msg,omitempty"` // 新規: エラーメッセージ
	TokenInfo *TokenInfo `json:"token_info,omitempty"` // 新規: トークン情報
}

// トークン情報構造体（サーバーと同じ）
type TokenInfo struct {
	TokenRaw       string    `json:"token_raw"`
	Email          string    `json:"email"`
	CreatedAt      time.Time `json:"created_at"`
	ExpireAt       time.Time `json:"expire_at"`
	LocalPort      int       `json:"local_port"`
	LocalIP        string    `json:"local_ip"`
	ProtocolType   string    `json:"protocol_type"`
	BandwidthLimit string    `json:"bandwidth_limit"`
	RemotePort     int       `json:"remote_port"`      // 新規: リモートポート
}

// プロキシ設定
type ProxyConfig struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
}

// FRPクライアント
type FRPClient struct {
	serverAddr     string
	serverConn     net.Conn
	token          string        // 新規: 認証トークン
	proxies        []ProxyConfig
	tokenInfo      *TokenInfo    // 新規: トークン情報
	localConns     map[string]net.Conn
	mutex          sync.RWMutex
	reconnectDelay time.Duration
}

func NewFRPClient(serverAddr, token string) *FRPClient {
	return &FRPClient{
		serverAddr:     serverAddr,
		token:         token,
		proxies:        []ProxyConfig{}, // 初期化時は空、認証後に設定
		localConns:     make(map[string]net.Conn),
		reconnectDelay: 60 * time.Second,
	}
}

func (c *FRPClient) Start() error {
	for {
		err := c.connect()
		if err != nil {
			log.Printf("Connection failed: %v", err)
			log.Printf("Retrying in %v...", c.reconnectDelay)
			time.Sleep(c.reconnectDelay)
			continue
		}

		err = c.handleConnection()
		if err != nil {
			log.Printf("Connection error: %v", err)
		}

		log.Printf("Disconnected from server. Retrying in %v...", c.reconnectDelay)
		time.Sleep(c.reconnectDelay)
	}
}

func (c *FRPClient) connect() error {
	conn, err := net.Dial("tcp", c.serverAddr)
	if err != nil {
		return err
	}

	c.serverConn = conn
	log.Printf("Connected to FRP server at %s", c.serverAddr)

	return c.login()
}

func (c *FRPClient) login() error {
	// 初期ログインでは空のプロキシ設定を送信（トークン認証のみ）
	msg := Message{
		Type:  MSG_TYPE_LOGIN,
		Token: c.token,
		Data:  []byte("{}"), // 空のJSON
	}

	encoder := json.NewEncoder(c.serverConn)
	if err := encoder.Encode(msg); err != nil {
		return err
	}

	// ログイン応答を待つ
	decoder := json.NewDecoder(c.serverConn)
	var response Message
	if err := decoder.Decode(&response); err != nil {
		return err
	}

		switch response.Type {
	case "login_success":
		// トークン情報を保存
		if response.TokenInfo != nil {
			c.tokenInfo = response.TokenInfo
			
			// トークン情報からプロキシ設定を構築
			c.buildProxyFromTokenInfo()
			
			log.Printf("Login successful with token info:")
			log.Printf("  Email: %s", c.tokenInfo.Email)
			log.Printf("  Protocol: %s", c.tokenInfo.ProtocolType)
			log.Printf("  Local: %s:%d", c.tokenInfo.LocalIP, c.tokenInfo.LocalPort)
			log.Printf("  Remote Port: %d", c.tokenInfo.RemotePort)
			log.Printf("  Bandwidth: %s", c.tokenInfo.BandwidthLimit)
		}

		share.IsConnection = true // 接続状態を更新
		share.PublicAddr = fmt.Sprintf("quickport.natyosu.com:%d", c.GetPublicPort())
		share.Route = fmt.Sprintf("localhost:%d <-----> quickport.natyosu.com:%d", c.GetLocalPort(), c.GetPublicPort())

		log.Printf("Configured %d proxies from token", len(c.proxies))
		if len(response.Data) > 0 {
			log.Printf("Server message: %s", string(response.Data))
		}
		for _, proxy := range c.proxies {
			log.Printf("  - %s: %s:%d -> :%d", proxy.Name, proxy.LocalIP, proxy.LocalPort, proxy.RemotePort)
		}
		return nil
	case "login_failed":
		return fmt.Errorf("login failed: %s", response.ErrorMsg)
	}

	return fmt.Errorf("login failed")
}

// トークン情報からプロキシ設定を構築
func (c *FRPClient) buildProxyFromTokenInfo() {
	if c.tokenInfo == nil {
		return
	}
	
	proxy := ProxyConfig{
		Name:       c.tokenInfo.ProtocolType,
		Type:       c.tokenInfo.ProtocolType,
		LocalIP:    c.tokenInfo.LocalIP,
		LocalPort:  c.tokenInfo.LocalPort,
		RemotePort: c.tokenInfo.RemotePort,
	}
	
	c.proxies = []ProxyConfig{proxy}
	
	log.Printf("Built proxy config from token: %s:%d -> :%d", 
		proxy.LocalIP, proxy.LocalPort, proxy.RemotePort)
}

func (c *FRPClient) handleConnection() error {
	decoder := json.NewDecoder(c.serverConn)

	for {
		var msg Message
		if err := decoder.Decode(&msg); err != nil {
			if err == io.EOF {
				return fmt.Errorf("server closed connection")
			}
			return err
		}

		switch msg.Type {
		case MSG_TYPE_NEW_CONN:
			go c.handleNewConnection(&msg)
		case MSG_TYPE_DATA:
			c.handleData(&msg)
		case MSG_TYPE_CLOSE:
			c.handleClose(&msg)
		case MSG_TYPE_KICK:
			share.IsConnection = false // 接続状態を更新
			share.IsRunningFrpc = false // FRPクライアントを停止
			log.Printf("Received kick message from server. Disconnecting...")
			return fmt.Errorf("kicked by server")
		}
	}
}

func (c *FRPClient) handleNewConnection(msg *Message) {
	// プロキシ設定を見つける
	var proxyConfig *ProxyConfig
	for _, proxy := range c.proxies {
		if proxy.Name == msg.ProxyName {
			proxyConfig = &proxy
			break
		}
	}

	if proxyConfig == nil {
		log.Printf("Unknown proxy: %s", msg.ProxyName)
		return
	}

	// ローカルサービスに接続
	localAddr := net.JoinHostPort(proxyConfig.LocalIP, fmt.Sprintf("%d", proxyConfig.LocalPort))
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("Failed to connect to local service %s: %v", localAddr, err)
		c.sendCloseMessage(msg.ConnID)
		return
	}

	c.mutex.Lock()
	c.localConns[msg.ConnID] = localConn
	c.mutex.Unlock()

	log.Printf("New proxy connection %s: %s -> %s", msg.ConnID, msg.ProxyName, localAddr)

	// ローカル接続からのデータを読み取り、サーバーに転送
	go c.forwardFromLocal(localConn, msg.ConnID)
}

func (c *FRPClient) forwardFromLocal(localConn net.Conn, connID string) {
	defer func() {
		localConn.Close()
		c.mutex.Lock()
		delete(c.localConns, connID)
		c.mutex.Unlock()
		c.sendCloseMessage(connID)
	}()

	buffer := make([]byte, 4096)
	for {
		n, err := localConn.Read(buffer)
		if err != nil {
			if err != io.EOF {
				log.Printf("Local connection read error: %v", err)
			}
			break
		}

		c.sendDataMessage(connID, buffer[:n])
	}
}

func (c *FRPClient) handleData(msg *Message) {
	c.mutex.RLock()
	localConn, exists := c.localConns[msg.ConnID]
	c.mutex.RUnlock()

	if exists {
		localConn.Write(msg.Data)
	}
}

func (c *FRPClient) handleClose(msg *Message) {
	c.mutex.Lock()
	if localConn, exists := c.localConns[msg.ConnID]; exists {
		localConn.Close()
		delete(c.localConns, msg.ConnID)
	}
	c.mutex.Unlock()

	log.Printf("Connection %s closed", msg.ConnID)
}

func (c *FRPClient) sendDataMessage(connID string, data []byte) {
	msg := Message{
		Type:   MSG_TYPE_DATA,
		ConnID: connID,
		Data:   data,
	}

	encoder := json.NewEncoder(c.serverConn)
	encoder.Encode(msg)
}

func (c *FRPClient) sendCloseMessage(connID string) {
	msg := Message{
		Type:   MSG_TYPE_CLOSE,
		ConnID: connID,
	}

	encoder := json.NewEncoder(c.serverConn)
	encoder.Encode(msg)
}

func (c *FRPClient) GetLocalPort() int {
	if len(c.proxies) > 0 {
		return c.proxies[0].LocalPort
	}
	return 0 // プロキシが設定されていない場合は0を返す
}

func (c *FRPClient) GetPublicPort() int {
	if len(c.proxies) > 0 {
		return c.proxies[0].RemotePort
	}
	return 0 // プロキシが設定されていない場合は0を返す
}
