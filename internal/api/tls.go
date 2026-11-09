package api

// HTTPS 自签证书：面板内一键开启，生成 ECDSA P-256 自签证书并写入配置。
// 证书 10 年有效，SAN 覆盖主机名、localhost 与本机全部网卡地址，
// 局域网设备导入一次即可长期信任。

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// DefaultTLSDir 自签证书的默认存放目录。
const DefaultTLSDir = "/etc/nanopanel/tls"

// tlsDirFor 返回证书存放目录（测试可注入 tlsDir 字段覆盖）。
func (s *Server) tlsDirFor() string {
	if s.tlsDir != "" {
		return s.tlsDir
	}
	return DefaultTLSDir
}

// GenerateSelfSignedCert 生成自签证书与私钥。hosts 为 SAN 列表。
func GenerateSelfSignedCert(certPath, keyPath string, hosts []string) error {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return fmt.Errorf("生成私钥: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return fmt.Errorf("生成序列号: %w", err)
	}
	tpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "NanoPanel", Organization: []string{"NanoPanel"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tpl.IPAddresses = append(tpl.IPAddresses, ip)
		} else {
			tpl.DNSNames = append(tpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &priv.PublicKey, priv)
	if err != nil {
		return fmt.Errorf("签发证书: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(certPath), 0750); err != nil {
		return fmt.Errorf("创建证书目录: %w", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return fmt.Errorf("编码私钥: %w", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if err := os.WriteFile(certPath, certPEM, 0644); err != nil {
		return fmt.Errorf("写入证书: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0600); err != nil {
		return fmt.Errorf("写入私钥: %w", err)
	}
	return nil
}

// LocalHostnames 汇总本机证书应覆盖的名称：主机名、localhost、全部网卡 IP。
func LocalHostnames() []string {
	hosts := []string{"localhost"}
	if h, err := os.Hostname(); err == nil && h != "" {
		hosts = append(hosts, h)
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return hosts
	}
	for _, ifc := range ifaces {
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				hosts = append(hosts, ipnet.IP.String())
			}
		}
	}
	return hosts
}

// TLSSettings 返回 HTTPS 配置状态与证书有效期。
func (s *Server) TLSSettings(w http.ResponseWriter, r *http.Request) {
	type tlsResp struct {
		Enabled  bool   `json:"enabled"`
		CertPath string `json:"cert_path,omitempty"`
		KeyPath  string `json:"key_path,omitempty"`
		Expires  string `json:"expires,omitempty"`
	}
	s.mu.RLock()
	resp := tlsResp{
		Enabled:  s.cfg.TLSCert != "" && s.cfg.TLSKey != "",
		CertPath: s.cfg.TLSCert,
		KeyPath:  s.cfg.TLSKey,
	}
	s.mu.RUnlock()
	if resp.Enabled {
		if data, err := os.ReadFile(resp.CertPath); err == nil {
			if block, _ := pem.Decode(data); block != nil {
				if c, err := x509.ParseCertificate(block.Bytes); err == nil {
					resp.Expires = c.NotAfter.Format("2006-01-02")
				}
			}
		}
	}
	ok(w, resp)
}

type tlsReq struct {
	Enabled bool `json:"enabled"`
}

// SaveTLSSettings 开启/关闭 HTTPS。开启时自动生成自签证书（已存在则复用），
// 保存配置后延迟数秒重启面板服务使其生效。
func (s *Server) SaveTLSSettings(w http.ResponseWriter, r *http.Request) {
	var req tlsReq
	if err := decodeJSON(w, r, &req); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}

	s.mu.Lock()
	certPath, keyPath := s.cfg.TLSCert, s.cfg.TLSKey
	s.mu.Unlock()

	if req.Enabled {
		if certPath == "" || keyPath == "" {
			certPath = filepath.Join(s.tlsDirFor(), "nanopanel.crt")
			keyPath = filepath.Join(s.tlsDirFor(), "nanopanel.key")
		}
		if _, err := os.Stat(certPath); os.IsNotExist(err) {
			if err := GenerateSelfSignedCert(certPath, keyPath, LocalHostnames()); err != nil {
				fail(w, http.StatusInternalServerError, "生成证书失败: "+err.Error())
				return
			}
		}
		if _, err := tls.LoadX509KeyPair(certPath, keyPath); err != nil {
			fail(w, http.StatusBadRequest, "证书文件不可用: "+err.Error())
			return
		}
	} else {
		certPath, keyPath = "", ""
	}

	s.mu.Lock()
	oldCert, oldKey := s.cfg.TLSCert, s.cfg.TLSKey
	s.cfg.TLSCert, s.cfg.TLSKey = certPath, keyPath
	if err := s.cfg.Save(s.configPath); err != nil {
		s.cfg.TLSCert, s.cfg.TLSKey = oldCert, oldKey
		s.mu.Unlock()
		fail(w, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	s.mu.Unlock()

	if req.Enabled {
		s.audit(r, "settings", "开启 HTTPS", "证书 "+certPath)
		// 延迟重启自身，让响应先送达浏览器。测试通过 selfRestart 注入空实现。
		if err := s.restartSelf(); err != nil {
			ok(w, map[string]string{
				"msg": "证书已生成，请手动重启面板使 HTTPS 生效",
			})
			return
		}
		ok(w, map[string]string{
			"msg": "证书已生成，面板将在数秒后自动重启。重启后请用 https://<设备IP>:" +
				fmt.Sprintf("%d 访问，浏览器首次需信任自签证书", s.cfg.Port()),
		})
		return
	}
	s.audit(r, "settings", "关闭 HTTPS", "")
	_ = s.restartSelf() // 关闭同样自动重启生效，保持行为对称
	ok(w, map[string]string{"msg": "已关闭 HTTPS，面板将在数秒后自动重启生效"})
}

// restartSelf 在响应送达后延迟重启面板服务；无 systemd 的环境静默失败。
func (s *Server) restartSelf() error {
	if s.selfRestart != nil {
		return s.selfRestart()
	}
	return exec.Command("sh", "-c", "sleep 3 && systemctl restart nanopanel").Start()
}
