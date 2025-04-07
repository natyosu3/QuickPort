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
	err = buildSource("./latest_release/QuickPort-main/")
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
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("failed to get executable path: %v", err)
	}

	execDir := filepath.Dir(execPath) // ディレクトリパスを取得
	// installしたGoのパスを取得
	goPath := filepath.Join(execDir, "go", "bin", "go.exe")

	// ソースディレクトリに移動
	if err := os.Chdir(sourceDir); err != nil {
		return fmt.Errorf("failed to change directory: %v", err)
	}

	fmt.Println("Building source code...")
	fmt.Println("Current directory:", sourceDir)

	// go.mod ファイルが存在しない場合は初期化
	if _, err := os.Stat("go.mod"); os.IsNotExist(err) {
		cmd := exec.Command(goPath, "mod", "init", "QuickPort")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to initialize go.mod: %v", err)
		}
	}

	// 依存関係を解決
	cmd := exec.Command(goPath, "mod", "tidy")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to tidy modules: %v", err)
	}

	if runtime.GOOS == "windows" {
		cmd = exec.Command(goPath, "build", "-o", "QuickPort.exe", "./cmd/QuickPort/main.go")
	} else {
		cmd = exec.Command("go", "build", "-o", "QuickPort", "./cmd/QuickPort/main.go")
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	fmt.Println("Building...")
	if err := cmd.Run(); err != nil { // ビルドエラーを確認
		return fmt.Errorf("failed to build QuickPort: %v", err)
	}

	if err := os.Chdir(execDir); err != nil {
		return fmt.Errorf("failed to change directory: %v", err)
	}

	// 自身の実行ファイルをリネーム
	if runtime.GOOS == "windows" {
		err = os.Rename("QuickPort.exe", "QuickPort_old.exe")
	} else {
		err = os.Rename("QuickPort", "QuickPort_old")
	}
	if err != nil {
		return fmt.Errorf("failed to rename old executable: %v", err)
	}

	newExePath := filepath.Join(sourceDir, "QuickPort.exe")

	err = os.Rename(newExePath, execPath)
	if err != nil {
		return fmt.Errorf("failed to move new QuickPort.exe: %v", err)
	}

	newExePath = filepath.Join(execDir, "QuickPort.exe")

	// クリーンアップ
	err = Cleanup()
	if err != nil {
		return fmt.Errorf("failed to clean up: %v", err)
	}

	if runtime.GOOS == "windows" {
		cmd := exec.Command(newExePath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start new QuickPort.exe: %v", err)
		}

		// 現在のプロセスを終了
		os.Exit(0)
	} else {
		// Linuxの場合の処理（必要に応じて追加）
		return fmt.Errorf("unsupported operation on non-Windows systems")
	}

	return nil
}

// アップデートに利用したファイルを削除する関数
func Cleanup() error {
	// アップデートに利用したファイルを削除
	goArchive := "go_archive.zip"
	if runtime.GOOS == "linux" {
		goArchive = "go_archive.tar.gz"
	}
	err := os.Remove(goArchive)
	if err != nil {
		return fmt.Errorf("failed to delete archive: %v", err)
	}

	releaseArchive := "latest_release.zip"
	if runtime.GOOS == "linux" {
		goArchive = "latest_release.tar.gz"
	}
	err = os.Remove(releaseArchive)
	if err != nil {
		return fmt.Errorf("failed to delete archive: %v", err)
	}

	err = os.RemoveAll("latest_release")
	if err != nil {
		return fmt.Errorf("failed to delete latest release folder: %v", err)
	}

	err = os.RemoveAll("go")
	if err != nil {
		return fmt.Errorf("failed to delete latest release folder: %v", err)
	}

	fmt.Println("Cleanup completed successfully!")
	return nil
}

func DeleteOldVersion() error {
	// 古いバージョンのファイルを削除
	oldExePath := "QuickPort_old.exe"
	if runtime.GOOS == "linux" {
		oldExePath = "QuickPort_old"
	}

	if _, err := os.Stat(oldExePath); err == nil {
		err := os.Remove(oldExePath)
		if err != nil {
			return fmt.Errorf("failed to delete old version: %v", err)
		}
		fmt.Println("Old version deleted successfully!")
	} else if os.IsNotExist(err) {
		fmt.Println("No old version found.")
	} else {
		return fmt.Errorf("failed to check old version: %v", err)
	}

	return nil
}
