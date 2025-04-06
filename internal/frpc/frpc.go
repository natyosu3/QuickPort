package frpc

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"

	"github.com/fatedier/frp/client"
	config "github.com/fatedier/frp/pkg/config/v1"
)

type tokenInfo struct {
	TokenID        string `json:"token_id"`
	Token          string `json:"token"`
	UserID         string `json:"user_id"`
	CreatedAt      string `json:"created_at"`
	ExpiresAt      string `json:"expires_at"`
	LocalPort      string `json:"local_port"`
	LocalIP        string `json:"local_ip"`
	ProtocolType   string `json:"protocol_type"`
	BandwidthLimit string `json:"bandwidth_limit"`
}

// tokenAuthorizationChannel 構造体
type TokenAuthorizationChannel struct {
	Status    string    `json:"status"`
	Message   string    `json:"message"`
	TokenInfo tokenInfo `json:"token_info"`
}

func StartFrpc(tokenInfo tokenInfo, remotePort int) {
	// frpc 設定をプログラム内で構築
	clientCfg := &config.ClientCommonConfig{
		ServerAddr: "163.44.96.225",
		ServerPort: 7676,
		User:       tokenInfo.UserID,
		Metadatas: map[string]string{
			"token": tokenInfo.Token,
		},
	}

	// localportをintに変換
	localPort, err := strconv.Atoi(tokenInfo.LocalPort)
	if err != nil {
		log.Fatalf("Failed to convert local port to int: %v", err)
	}

	proxyCfg := &config.TCPProxyConfig{
		ProxyBaseConfig: config.ProxyBaseConfig{
			ProxyBackend: config.ProxyBackend{
				LocalIP:   "127.0.0.1",
				LocalPort: localPort,
			},
		},
		RemotePort: remotePort,
	}

	serviceOptions := client.ServiceOptions{
		Common: clientCfg,
		ProxyCfgs: []config.ProxyConfigurer{
			proxyCfg,
		},
	}

	// frpc クライアントを作成
	frpcService, err := client.NewService(serviceOptions)
	if err != nil {
		log.Fatalf("Failed to create frpc client: %v", err)
	}

	// コンテキストを作成
	ctx := context.Background()

	err = frpcService.Run(ctx)
	if err != nil {
		log.Fatalf("Failed to start frpc: %v", err)
	}
	log.Println("frpc is running...")

}

func SendTokenValidityRequest(token string, ch chan TokenAuthorizationChannel) {
	// HTTPSリクエストを送信
	endpoint := "https://quick-port-auth.natyosu.com/auth/token-validity"
	req, err := http.NewRequest("GET", endpoint, nil)
	if err != nil {
		log.Printf("HTTPリクエストの作成に失敗しました: %v", err)
		ch <- TokenAuthorizationChannel{
			Status:  "ERROR",
			Message: "HTTPリクエストの作成に失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}
	req.Header.Set("token", token)

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("HTTPリクエストの送信に失敗しました: %v", err)
		ch <- TokenAuthorizationChannel{
			Status:  "ERROR",
			Message: "HTTPリクエストの送信に失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}
	defer resp.Body.Close()

	// レスポンスボディを読み取る
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("レスポンスボディの読み取りに失敗しました: %v", err)
		ch <- TokenAuthorizationChannel{
			Status:  "ERROR",
			Message: "レスポンスボディの読み取りに失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}

	// レスポンスボディをパース
	var parsedResponse TokenAuthorizationChannel
	err = json.Unmarshal(respBody, &parsedResponse)
	if err != nil {
		log.Printf("129: レスポンスボディのパースに失敗しました: %v", err)
		ch <- TokenAuthorizationChannel{
			Status:  "ERROR",
			Message: "129: レスポンスボディのパースに失敗しました: " + err.Error(),
		}
		close(ch)
		return
	}

	if parsedResponse.Status == "ERROR" {
		ch <- TokenAuthorizationChannel{
			Status:  "ERROR",
			Message: parsedResponse.Message,
		}
	} else {
		ch <- parsedResponse
	}
	close(ch) // チャンネルを閉じる
}

// func main() {
// 	// frpc 設定をプログラム内で構築
// 	clientCfg := &config.ClientCommonConfig{
// 		ServerAddr: "localhost",
// 		ServerPort: 7000,
// 		User:       "test",
// 		Metadatas: map[string]string{
// 			"token": "5d256792c92ef54215a21a17e2dde83e0caa95c328d8e7efc7d8fc534a4ee09c5f2b79f29226d7c45576b899a6ab118a2a5e0d54a038fc355cec47f467aa1141",
// 		},
// 	}

// 	proxyCfg := &config.TCPProxyConfig{
// 		ProxyBaseConfig: config.ProxyBaseConfig{
// 			ProxyBackend: config.ProxyBackend{
// 				LocalIP:   "127.0.0.1",
// 				LocalPort: 11111,
// 			},
// 		},
// 		RemotePort: 0, // 0 にすると frps 側でポートを決定
// 	}

// 	serviceOptions := client.ServiceOptions{
// 		Common: clientCfg,
// 		ProxyCfgs: []config.ProxyConfigurer{
// 			proxyCfg,
// 		},
// 	}

// 	// frpc クライアントを作成
// 	frpcService, err := client.NewService(serviceOptions)
// 	if err != nil {
// 		log.Fatalf("Failed to create frpc client: %v", err)
// 	}

// 	// コンテキストを作成
// 	ctx := context.Background()

// 	err = frpcService.Run(ctx)
// 	if err != nil {
// 		log.Fatalf("Failed to start frpc: %v", err)
// 	}
// 	log.Println("frpc is running...")

// }
