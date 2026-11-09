package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credentials 保存首次安装时生成的登录凭据，供 npctl user-info 查询。
// 文件权限为 0600，且仅 root 可读。
type Credentials struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// CredentialsPath 返回与配置文件同目录的凭据文件路径。
func CredentialsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "credentials.json")
}

// SaveCredentials 写入初始凭据（0600）。
func SaveCredentials(configPath, username, password string) error {
	now := time.Now().Format(time.RFC3339)
	c := Credentials{Username: username, Password: password, CreatedAt: now}
	return writeCredentials(configPath, &c)
}

// UpdateCredentialsPassword 在面板内改密后同步凭据文件，保证 npctl user-info 准确。
// 凭据文件被删除时自动重建。
func UpdateCredentialsPassword(configPath, password string) error {
	c, err := LoadCredentials(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			username := DefaultUsername
			if cfg, e := Load(configPath); e == nil && cfg.Username != "" {
				username = cfg.Username
			}
			return SaveCredentials(configPath, username, password)
		}
		return err
	}
	c.Password = password
	c.UpdatedAt = time.Now().Format(time.RFC3339)
	return writeCredentials(configPath, &c)
}

// LoadCredentials 读取凭据文件。
func LoadCredentials(configPath string) (Credentials, error) {
	data, err := os.ReadFile(CredentialsPath(configPath))
	if err != nil {
		return Credentials{}, err
	}
	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return Credentials{}, fmt.Errorf("解析凭据文件: %w", err)
	}
	return c, nil
}

func writeCredentials(configPath string, c *Credentials) error {
	path := CredentialsPath(configPath)
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return fmt.Errorf("创建配置目录: %w", err)
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0600)
}
