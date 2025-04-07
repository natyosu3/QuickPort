package util

import (
	"QuickPort/share"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Masterminds/semver/v3"
)

// 最新版があるかどうかを確認する関数
func GetNewVersion(currentVer string) (string, error) {
	// githubのAPIを使用して最新バージョンを取得
	request, err := http.NewRequest("GET", share.RELEASE_ENDPOINT, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Accept", "application/vnd.github.v3+json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to get latest version: %s", response.Status)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return "", err
	}
	// セマンティックバージョンを解析
	currentVersion, err := semver.NewVersion(currentVer)
	if err != nil {
		return "", fmt.Errorf("invalid current version: %s", err)
	}

	latestVersion, err := semver.NewVersion(result.TagName)
	if err != nil {
		return "", fmt.Errorf("invalid latest version: %s", err)
	}

	// バージョンを比較
	if latestVersion.GreaterThan(currentVersion) {
		return result.TagName, nil
	}
	return "", nil
}
