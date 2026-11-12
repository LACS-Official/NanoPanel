//go:build linux

package api

// Linux 伪终端（PTY）实现。通过 /dev/ptmx 分配主从设备，让 shell 认为
// 自己连接着真实终端，从而支持 vim / top / sudo 密码输入 / Tab 补全 /
// 作业控制等完整交互能力；窗口尺寸变化时向前台进程组发送 SIGWINCH。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// ptyWinsize 对应 Linux struct winsize。
type ptyWinsize struct {
	Row, Col, Xpixel, Ypixel uint16
}

// terminalProcess 是平台无关的终端进程抽象，由 pty_linux.go / pty_other.go
// 各提供一份实现；terminal.go 只依赖这里列出的方法集。
type terminalProcess struct {
	master *os.File
	slave  *os.File
	cmd    *exec.Cmd

	pty        bool
	closeOnce  sync.Once
	closeError error
}

func ptyIoctl(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

func setPTYSize(f *os.File, cols, rows uint16) error {
	if f == nil {
		return errors.New("终端尚未就绪")
	}
	ws := ptyWinsize{Row: rows, Col: cols}
	return ptyIoctl(f.Fd(), uintptr(syscall.TIOCSWINSZ), unsafe.Pointer(&ws))
}

func openPTY(cols, rows uint16) (*os.File, *os.File, error) {
	master, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, fmt.Errorf("打开 /dev/ptmx 失败: %w", err)
	}
	unlock := int32(0)
	if err := ptyIoctl(master.Fd(), uintptr(syscall.TIOCSPTLCK), unsafe.Pointer(&unlock)); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("解锁伪终端失败: %w", err)
	}
	ptn := int32(0)
	if err := ptyIoctl(master.Fd(), uintptr(syscall.TIOCGPTN), unsafe.Pointer(&ptn)); err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("获取伪终端编号失败: %w", err)
	}
	slave, err := os.OpenFile(fmt.Sprintf("/dev/pts/%d", ptn), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		master.Close()
		return nil, nil, fmt.Errorf("打开伪终端从设备失败: %w", err)
	}
	if cols > 0 && rows > 0 {
		_ = setPTYSize(master, cols, rows)
	}
	return master, slave, nil
}

// detectShell 挑选一个可用的交互式 shell。
func detectShell(preferred string) (string, []string) {
	candidates := make([]string, 0, 6)
	if preferred != "" {
		candidates = append(candidates, preferred)
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		candidates = append(candidates, sh)
	}
	candidates = append(candidates, "/bin/bash", "/bin/ash", "/bin/sh")
	for _, sh := range candidates {
		if !strings.HasPrefix(sh, "/") {
			continue // 只接受绝对路径，避免 PATH 劫持
		}
		st, err := os.Stat(sh)
		if err != nil || st.IsDir() || st.Mode()&0111 == 0 {
			continue
		}
		// -i 启用交互模式（提示符、readline、历史）；-l 载入 profile 补齐 PATH。
		args := []string{"-i"}
		if strings.HasSuffix(sh, "bash") || strings.HasSuffix(sh, "zsh") {
			args = []string{"-i", "-l"}
		}
		return sh, args
	}
	return "", nil
}

// startTerminal 创建伪终端并启动交互式 shell。
func startTerminal(shell, cwd string, cols, rows uint16, extraEnv []string) (*terminalProcess, error) {
	sh, args := detectShell(shell)
	if sh == "" {
		return nil, errors.New("未找到可用的 shell")
	}
	master, slave, err := openPTY(cols, rows)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(sh, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	// Setsid + Setctty 让 shell 成为会话首进程并持有控制终端，
	// 这样 Ctrl+C / Ctrl+Z 等作业控制信号才能正确投递到前台进程组。
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		slave.Close()
		master.Close()
		return nil, fmt.Errorf("启动 shell 失败: %w", err)
	}
	// 父进程必须关闭自己的 slave 副本，否则 shell 退出后 master 读不到 EOF。
	slave.Close()
	return &terminalProcess{master: master, cmd: cmd, pty: true}, nil
}

// Read 读取 shell 输出。所有从设备关闭时 Linux 返回 EIO，按 EOF 处理。
func (p *terminalProcess) Read(buf []byte) (int, error) {
	n, err := p.master.Read(buf)
	if errors.Is(err, syscall.EIO) {
		return n, io.EOF
	}
	return n, err
}

func (p *terminalProcess) Write(data []byte) (int, error) {
	return p.master.Write(data)
}

func (p *terminalProcess) Resize(cols, rows uint16) error {
	if cols == 0 || rows == 0 {
		return nil
	}
	if err := setPTYSize(p.master, cols, rows); err != nil {
		return err
	}
	if p.cmd != nil && p.cmd.Process != nil {
		// 负值表示向整个进程组发信号；非交互降级实现无需处理。
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGWINCH)
	}
	return nil
}

func (p *terminalProcess) Wait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}

// Close 关闭主设备并尝试优雅终止 shell；重复调用安全。
func (p *terminalProcess) Close() error {
	p.closeOnce.Do(func() {
		if p.cmd != nil && p.cmd.Process != nil {
			_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGHUP)
		}
		if p.master != nil {
			p.closeError = p.master.Close()
		}
		if p.slave != nil {
			_ = p.slave.Close()
		}
	})
	return p.closeError
}

// Kill 在优雅退出超时后强制结束整个进程组。
func (p *terminalProcess) Kill() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}

// IsPTY 报告当前是否使用真实伪终端，供前端提示能力差异。
func (p *terminalProcess) IsPTY() bool { return p.pty }
