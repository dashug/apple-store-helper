package services

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

const (
	settingsFileName = "user_settings.json"
	settingsDirName  = "apple-store-helper"
)

// 便于测试覆盖
var configDirFn = os.UserConfigDir

type UserSettings struct {
	SelectedArea    string `json:"selected_area"`
	SelectedStore   string `json:"selected_store"`
	SelectedProduct string `json:"selected_product"`
	BarkNotifyUrl   string `json:"bark_notify_url"`

	// NotifyUrls 是除 Bark 之外的通知地址，每行一个
	NotifyUrls string `json:"notify_urls"`

	ListenItems map[string]ListenItem `json:"listen_items"`

	// PollIntervalSeconds 为 0 表示沿用默认间隔（兼容旧配置文件）
	PollIntervalSeconds int `json:"poll_interval_seconds"`

	// WindowWidth / WindowHeight 为 0 表示使用默认尺寸
	WindowWidth  int `json:"window_width"`
	WindowHeight int `json:"window_height"`
}

// SettingsPath 返回配置文件的绝对路径，供界面与命令行提示用户
func SettingsPath() (string, error) {
	return settingsPath()
}

// settingsPath 返回配置文件的绝对路径
//
// 不能使用相对路径：macOS 下从访达双击 .app 启动时工作目录是 /，
// 写入会直接失败（read-only file system），配置永远存不下来。
func settingsPath() (string, error) {
	dir, err := configDirFn()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, settingsDirName, settingsFileName), nil
}

// 保存配置到本地文件 SaveSettings saves settings to a file
func SaveSettings(settings UserSettings) error {
	path, err := settingsPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

// 加载缓存配置 LoadSettings loads settings from a file
func LoadSettings() (UserSettings, error) {
	var settings UserSettings

	path, err := settingsPath()
	if err != nil {
		return settings, err
	}

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		// 旧版本把配置写在工作目录下。Windows 下双击 exe 时工作目录是 exe
		// 所在目录，确实能写成功，因此这里兼容读取，下次保存会落到新位置。
		data, err = os.ReadFile(settingsFileName)
	}
	if err != nil {
		return settings, err
	}

	err = json.Unmarshal(data, &settings)
	return settings, err
}

// 清空缓存配置 ClearSettings removes the settings file
func ClearSettings() error {
	path, err := settingsPath()
	if err != nil {
		return err
	}

	// 旧位置的文件也要清掉，否则下次启动又会被兼容逻辑读出来
	if err := os.Remove(settingsFileName); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}
