package update

import (
	"QuickPort/share"
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// GitHubから最新リリースをダウンロードしてビルドする関数
func DownloadAndBuildLatestRelease() error {
	// 1. Goバイナリをダウンロードしてインストール
	err := downloadAndInstallGo()
	if err != nil {
		return fmt.Errorf("failed to install Go: %v", err)
	}

	// 2. 最新リリースをダウンロード
	err = DownloadLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to download and build latest release: %v", err)
	}

	fmt.Println("Update completed successfully!")
	return nil
}

// Goバイナリをダウンロードしてインストールする関数
func downloadAndInstallGo() error {
	goDownloadURL := "https://go.dev/dl/go1.23.8.windows-amd64.zip" // 必要に応じてバージョンを変更
	if runtime.GOOS == "linux" {
		goDownloadURL = "https://go.dev/dl/go1.23.8.linux-amd64.tar.gz"
	}

	fmt.Printf("Downloading Go from: %s\n", goDownloadURL)

	// Goバイナリをダウンロード
	resp, err := http.Get(goDownloadURL)
	if err != nil {
		return fmt.Errorf("failed to download Go: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download Go: status code %d", resp.StatusCode)
	}

	// ダウンロードしたファイルを保存
	goArchive := "go_archive.zip"
	if runtime.GOOS == "linux" {
		goArchive = "go_archive.tar.gz"
	}
	out, err := os.Create(goArchive)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save Go archive: %v", err)
	}

	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	// 解凍してインストール
	if runtime.GOOS == "windows" {
		err = unzip(goArchive, dir)
	} else {
		err = untar(goArchive, dir)
	}
	if err != nil {
		return fmt.Errorf("failed to extract Go archive: %v", err)
	}

	// パスを設定
	if runtime.GOOS == "windows" {
		os.Setenv("PATH", dir+"Go\\bin;"+os.Getenv("PATH"))
	} else {
		os.Setenv("PATH", dir+"/go/bin:"+os.Getenv("PATH"))
	}

	fmt.Println("Go installed successfully!")
	return nil
}

// GitHubから最新リリースをダウンロードする関数
func DownloadLatestRelease() error {
	latest := share.RELEASE_ENDPOINT

	// 実行中のOSとアーキテクチャを取得
	currentOS := runtime.GOOS
	arch := runtime.GOARCH

	// サポートされていないOSやアーキテクチャの場合はエラーを返す
	if currentOS != "windows" && currentOS != "linux" {
		return fmt.Errorf("unsupported OS: %s", currentOS)
	}
	if arch != "amd64" && arch != "arm64" {
		return fmt.Errorf("unsupported architecture: %s", arch)
	}

	// 最新リリースのバージョンを取得
	resp, err := http.Get(latest)
	if err != nil {
		return fmt.Errorf("failed to get latest release: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to get latest release: status code %d", resp.StatusCode)
	}

	var result struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("failed to decode latest release: %v", err)
	}

	// ダウンロードURLを生成
	downloadURL := fmt.Sprintf("https://github.com/natyosu3/QuickPort/releases/download/%s/%s-%s.zip", result.TagName, currentOS, arch)

	// ダウンロードURLを生成
	fmt.Printf("Downloading from: %s\n", downloadURL)

	// ファイルをダウンロード
	resp, err = http.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("failed to download release: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to download release: status code %d", resp.StatusCode)
	}

	// ダウンロードしたファイルを保存
	outputFile := "latest_release.zip"
	out, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create file: %v", err)
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("failed to save file: %v", err)
	}

	// ダウンロードしたファイルを解凍
	err = unzip(outputFile, "./latest_release")
	if err != nil {
		return fmt.Errorf("failed to unzip file: %v", err)
	}

	// 解凍したソースコードをビルド
	err = buildSource("./latest_release/QuickPort/cmd/QuickPort/main.go")
	if err != nil {
		return fmt.Errorf("failed to build source: %v", err)
	}

	return nil
}

// ZIPファイルを解凍する関数
func unzip(src, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fPath := filepath.Join(dest, f.Name)
		if f.FileInfo().IsDir() {
			os.MkdirAll(fPath, os.ModePerm)
			continue
		}

		if err := os.MkdirAll(filepath.Dir(fPath), os.ModePerm); err != nil {
			return err
		}

		outFile, err := os.OpenFile(fPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}

		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// TAR.GZファイルを解凍する関数（Linux用）
func untar(src, dest string) error {
	cmd := exec.Command("tar", "-xzf", src, "-C", dest)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to untar file: %v", err)
	}
	return nil
}

// ソースコードをビルドする関数
func buildSource(sourceDir string) error {
	cmd := exec.Command("go", "build", "-o", "QuickPort", sourceDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to build source: %v", err)
	}
	return nil
}
