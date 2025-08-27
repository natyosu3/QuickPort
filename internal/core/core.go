package core

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"QuickPort/share"
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
	Protocol  string `json:"protocol,omitempty"`        // 新規: プロトコルタイプ（tcp/udp）
	UDPAddr   string `json:"udp_addr,omitempty"`        // 新規: UDP送信元アドレス
}

// トークン情報構造体（サーバーと同じ）
type TokenInfo struct {
	TokenRaw       string    `json:"token_raw"`
	Email          string    `json:"email"`
	CreatedAt      time.Time `json:"created_at"`
	ExpireAt       time.Time `json:"expire_at"`
	LocalPort      int       `json:"local_port"`
	LocalIP        string    `json:"local_ip"`
	RemotePort     int       `json:"remote_port"`
	ProtocolType   string    `json:"protocol_type"`
	BandwidthLimit string    `json:"bandwidth_limit"`
}

// プロキシ設定
type ProxyConfig struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	LocalIP    string `json:"local_ip"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Protocol   string `json:"protocol"`
}

// FRPクライアント
type FRPClient struct {
	serverAddr     string
	serverConn     net.Conn
	token          string        // 新規: 認証トークン
	proxies        []ProxyConfig
	tokenInfo      *TokenInfo    // 新規: トークン情報
	localConns     map[string]net.Conn
	udpConns       map[string]*net.UDPConn     // 新規: UDP用ローカル接続
	mutex          sync.RWMutex
	reconnectDelay time.Duration
}

func NewFRPClient(serverAddr, token string) *FRPClient {
	// FRPクライアントのログを完全に無効化（Bubble Teaとの競合を避けるため）
	log.SetOutput(io.Discard)
	
	return &FRPClient{
		serverAddr:     serverAddr,
		token:         token,
		proxies:        []ProxyConfig{}, // 初期化時は空、認証後に設定
		localConns:     make(map[string]net.Conn),
		udpConns:       make(map[string]*net.UDPConn), // 新規: UDP用接続マップ
		reconnectDelay: 5 * time.Second,
	}
}

func (c *FRPClient) Start() error {
	for {
		err := c.connect()
		if err != nil {
			share.IsConnection = false // 接続失敗時にフラグをリセット
			share.IsRunningFrpc = false // FRPクライアント実行フラグもリセット
			share.PublicAddr = ""      // 公開アドレス情報をクリア
			share.Route = ""           // ルート情報をクリア
			log.Printf("Connection failed: %v", err)
			log.Printf("Retrying in %v...", c.reconnectDelay)
			time.Sleep(c.reconnectDelay)
			continue
		}

		err = c.handleConnection()
		if err != nil {
			share.IsConnection = false // 接続エラー時にフラグをリセット
			share.IsRunningFrpc = false // FRPクライアント実行フラグもリセット
			share.PublicAddr = ""      // 公開アドレス情報をクリア
			share.Route = ""           // ルート情報をクリア
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
		// 接続フラグを設定
		share.IsConnection = true
		share.IsRunningFrpc = true  // FRPクライアント実行フラグも設定
		
		// トークン情報を保存
		if response.TokenInfo != nil {
			c.tokenInfo = response.TokenInfo
			
			// トークン情報からプロキシ設定を構築
			c.buildProxyFromTokenInfo()
			
			// 公開アドレス情報を設定
			share.PublicAddr = fmt.Sprintf("quickport.natyosu.com:%d", c.tokenInfo.RemotePort)
			share.Route = fmt.Sprintf("%s localhost:%d <-----> quickport.natyosu.com:%d", 
				strings.ToUpper(c.tokenInfo.ProtocolType), 
				c.tokenInfo.LocalPort, 
				c.tokenInfo.RemotePort)
			
			log.Printf("Setting share values: PublicAddr=%s, Route=%s", share.PublicAddr, share.Route)
			
			log.Printf("Login successful with token info:")
			log.Printf("  Email: %s", c.tokenInfo.Email)
			log.Printf("  Protocol: %s", c.tokenInfo.ProtocolType)
			log.Printf("  Local: %s:%d", c.tokenInfo.LocalIP, c.tokenInfo.LocalPort)
			log.Printf("  Remote: %s", share.PublicAddr)
			log.Printf("  Bandwidth: %s", c.tokenInfo.BandwidthLimit)
		}
		
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
	
	// サーバーから送信されたRemotePortを使用
	remotePort := c.tokenInfo.RemotePort
	if remotePort == 0 {
		// RemotePortが設定されていない場合のフォールバック
		remotePort = c.tokenInfo.LocalPort + 10000
		log.Printf("Warning: RemotePort not provided by server, using fallback: %d", remotePort)
	}
	
	proxy := ProxyConfig{
		Name:       c.tokenInfo.ProtocolType,
		Type:       c.tokenInfo.ProtocolType,
		LocalIP:    c.tokenInfo.LocalIP,
		LocalPort:  c.tokenInfo.LocalPort,
		RemotePort: remotePort,
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

	// プロトコルに応じて処理を分岐
	protocol := msg.Protocol
	if protocol == "" {
		// プロトコルが指定されていない場合は、プロキシ設定から判断
		protocol = strings.ToLower(proxyConfig.Type)
	}

	switch protocol {
	case "udp":
		c.handleNewUDPConnection(msg, proxyConfig)
	default:
		// デフォルトはTCP
		c.handleNewTCPConnection(msg, proxyConfig)
	}
}

func (c *FRPClient) handleNewTCPConnection(msg *Message, proxyConfig *ProxyConfig) {
	// ローカルサービスに接続
	localAddr := net.JoinHostPort(proxyConfig.LocalIP, fmt.Sprintf("%d", proxyConfig.LocalPort))
	localConn, err := net.Dial("tcp", localAddr)
	if err != nil {
		log.Printf("Failed to connect to local TCP service %s: %v", localAddr, err)
		c.sendCloseMessage(msg.ConnID)
		return
	}

	c.mutex.Lock()
	c.localConns[msg.ConnID] = localConn
	c.mutex.Unlock()

	log.Printf("New TCP proxy connection %s: %s -> %s", msg.ConnID, msg.ProxyName, localAddr)

	// ローカル接続からのデータを読み取り、サーバーに転送
	go c.forwardFromLocalTCP(localConn, msg.ConnID)
}

func (c *FRPClient) handleNewUDPConnection(msg *Message, proxyConfig *ProxyConfig) {
	// UDPの場合、既に接続が作成されている可能性があるためチェック
	c.mutex.RLock()
	_, exists := c.udpConns[msg.ConnID]
	c.mutex.RUnlock()
	
	if exists {
		log.Printf("UDP connection %s already exists, skipping creation", msg.ConnID)
		return
	}

	// UDPはコネクションレスなので、ローカルUDP接続を作成
	localAddr := net.JoinHostPort(proxyConfig.LocalIP, fmt.Sprintf("%d", proxyConfig.LocalPort))
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		log.Printf("Failed to resolve UDP address %s: %v", localAddr, err)
		c.sendCloseMessage(msg.ConnID)
		return
	}

	udpConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Printf("Failed to create UDP connection to %s: %v", localAddr, err)
		c.sendCloseMessage(msg.ConnID)
		return
	}

	// UDPコネクションにタイムアウトを設定
	udpConn.SetReadDeadline(time.Now().Add(30 * time.Second))

	c.mutex.Lock()
	c.udpConns[msg.ConnID] = udpConn
	c.mutex.Unlock()

	log.Printf("New UDP proxy connection %s: %s -> %s", msg.ConnID, msg.ProxyName, localAddr)
	log.Printf("UDP connection established for %s, waiting for data...", msg.ConnID)

	// UDPは最初のデータパケットが来た時点で接続とみなし、応答を待つ
	go c.forwardFromLocalUDP(udpConn, msg.ConnID)
}

func (c *FRPClient) forwardFromLocalTCP(localConn net.Conn, connID string) {
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
				log.Printf("Local TCP connection read error: %v", err)
			}
			break
		}

		c.sendDataMessage(connID, buffer[:n], "tcp")
	}
}

func (c *FRPClient) forwardFromLocalUDP(udpConn *net.UDPConn, connID string) {
	defer func() {
		udpConn.Close()
		c.mutex.Lock()
		delete(c.udpConns, connID)
		c.mutex.Unlock()
		log.Printf("UDP connection %s closed", connID)
	}()

	// UDPコネクションにタイムアウトを設定（30秒）
	udpConn.SetReadDeadline(time.Now().Add(30 * time.Second))

	buffer := make([]byte, 65535) // UDPの最大パケットサイズ
	for {
		n, err := udpConn.Read(buffer)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				log.Printf("UDP connection %s timed out, closing", connID)
			} else if err != io.EOF {
				log.Printf("Local UDP connection read error: %v", err)
			}
			break
		}

		log.Printf("UDP connection %s received %d bytes from local service", connID, n)
		c.sendDataMessage(connID, buffer[:n], "udp")
		
		// タイムアウトを延長
		udpConn.SetReadDeadline(time.Now().Add(30 * time.Second))
	}
}

func (c *FRPClient) handleData(msg *Message) {
	// プロトコルをチェック
	if msg.Protocol == "udp" {
		c.handleUDPData(msg)
	} else {
		// デフォルトはTCP
		c.handleTCPData(msg)
	}
}

func (c *FRPClient) handleTCPData(msg *Message) {
	c.mutex.RLock()
	localConn, exists := c.localConns[msg.ConnID]
	c.mutex.RUnlock()

	if exists {
		localConn.Write(msg.Data)
	}
}

func (c *FRPClient) handleUDPData(msg *Message) {
	log.Printf("Received UDP data for connection %s: %d bytes", msg.ConnID, len(msg.Data))
	
	c.mutex.RLock()
	udpConn, exists := c.udpConns[msg.ConnID]
	c.mutex.RUnlock()

	if !exists {
		log.Printf("UDP connection %s not found, creating new connection", msg.ConnID)
		// UDPの場合は接続を自動作成
		c.createUDPConnectionFromConnID(msg.ConnID)
		
		// 再度接続を取得
		c.mutex.RLock()
		udpConn, exists = c.udpConns[msg.ConnID]
		c.mutex.RUnlock()
	}

	if exists && udpConn != nil {
		log.Printf("Forwarding UDP data to local service: %d bytes", len(msg.Data))
		udpConn.Write(msg.Data)
	} else {
		log.Printf("Failed to handle UDP data for connection %s", msg.ConnID)
	}
}

func (c *FRPClient) createUDPConnectionFromConnID(connID string) {
	// connIDからプロキシ名を抽出（例: "udp_udp_127.0.0.1:4000_1756126385992784300" -> "udp"）
	parts := strings.Split(connID, "_")
	if len(parts) < 2 {
		log.Printf("Invalid connection ID format: %s", connID)
		return
	}
	
	proxyName := parts[0]
	
	// プロキシ設定を見つける
	var proxyConfig *ProxyConfig
	for _, proxy := range c.proxies {
		if proxy.Name == proxyName {
			proxyConfig = &proxy
			break
		}
	}

	if proxyConfig == nil {
		log.Printf("Unknown proxy for connection %s: %s", connID, proxyName)
		return
	}

	// ローカルUDPサービスに接続
	localAddr := net.JoinHostPort(proxyConfig.LocalIP, fmt.Sprintf("%d", proxyConfig.LocalPort))
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		log.Printf("Failed to resolve local UDP address %s: %v", localAddr, err)
		return
	}

	udpConn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		log.Printf("Failed to connect to local UDP service %s: %v", localAddr, err)
		return
	}

	// UDPコネクションにタイムアウトを設定
	udpConn.SetReadDeadline(time.Now().Add(30 * time.Second))

	c.mutex.Lock()
	c.udpConns[connID] = udpConn
	c.mutex.Unlock()

	log.Printf("Auto-created UDP connection %s: %s -> %s", connID, proxyName, localAddr)

	// UDPレスポンスを監視
	go c.forwardFromLocalUDP(udpConn, connID)
}

func (c *FRPClient) handleClose(msg *Message) {
	c.mutex.Lock()
	// TCPコネクションをクローズ
	if localConn, exists := c.localConns[msg.ConnID]; exists {
		localConn.Close()
		delete(c.localConns, msg.ConnID)
	}
	
	// UDPコネクションをクローズ
	if udpConn, exists := c.udpConns[msg.ConnID]; exists {
		udpConn.Close()
		delete(c.udpConns, msg.ConnID)
	}
	c.mutex.Unlock()

	log.Printf("Connection %s closed", msg.ConnID)
}

func (c *FRPClient) sendDataMessage(connID string, data []byte, protocol ...string) {
	msg := Message{
		Type:   MSG_TYPE_DATA,
		ConnID: connID,
		Data:   data,
	}

	// プロトコル情報が提供されている場合は設定
	if len(protocol) > 0 {
		msg.Protocol = protocol[0]
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
