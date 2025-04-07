package util

import (
	"QuickPort/share"
	"encoding/json"
	"fmt"
	"net/http"
)

// 最新版があるかどうかを確認する関数
func CheckNewVersion(currentVer string) (bool, error) {
	// githubのAPIを使用して最新バージョンを取得
	request, err := http.NewRequest("GET", share.RELEASE_ENDPOINT, nil)
	if err != nil {
		return false, err
	}
	request.Header.Set("Accept", "application/vnd.github.v3+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return false, fmt.Errorf("failed to get latest version: %s", response.Status)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return false, err
	}

	latestVer := result.TagName
	// バージョンを比較
	if latestVer > currentVer {
		return true, nil
	}
	return false, nil
}
