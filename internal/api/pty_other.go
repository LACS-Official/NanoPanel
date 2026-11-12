//go:build !linux

package api

// 非 Linux 平台（主要用于本地开发与 Windows CI）的降级实现：
// 用匿名管道连接 shell，可执行非交互式命令，但没有控制终端，
// 因此 vim / top / sudo 等全屏或需要 TTY 的程序不可用。
// 生产部署目标均为 Linux，此处保证跨平台可编译、行为可预期。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

type terminalProcess struct {
	inW  *os.File
	outR *os.File
	cmd  *exec.Cmd

	pty        bool
	closeOnce  sync.Once
	closeError error
}

func detectShell(preferred string) (string, []string) {
	if preferred != "" {
		if st, err := os.Stat(preferred); err == nil && !st.IsDir() {
			return preferred, []string{"-i"}
		}
	}
	for _, sh := range []string{"/bin/bash", "/bin/sh", "/bin/ash"} {
		if st, err := os.Stat(sh); err == nil && !st.IsDir() {
			return sh, []string{"-i"}
		}
	}
	return "", nil
}

func startTerminal(shell, cwd string, cols, rows uint16, extraEnv []string) (*terminalProcess, error) {
	sh, args := detectShell(shell)
	if sh == "" {
		return nil, errors.New("未找到可用的 shell")
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("创建输入管道失败: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		inR.Close()
		inW.Close()
		return nil, fmt.Errorf("创建输出管道失败: %w", err)
	}
	cmd := exec.Command(sh, args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, outW
	if err := cmd.Start(); err != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, fmt.Errorf("启动 shell 失败: %w", err)
	}
	inR.Close()
	outW.Close()
	return &terminalProcess{inW: inW, outR: outR, cmd: cmd}, nil
}

func (p *terminalProcess) Read(buf []byte) (int, error) { return p.outR.Read(buf) }

func (p *terminalProcess) Write(data []byte) (int, error) { return p.inW.Write(data) }

// Resize 管道模式没有终端尺寸概念，返回 nil 表示已忽略。
func (p *terminalProcess) Resize(cols, rows uint16) error { return nil }

func (p *terminalProcess) Wait() error {
	if p.cmd == nil {
		return nil
	}
	return p.cmd.Wait()
}

func (p *terminalProcess) Close() error {
	p.closeOnce.Do(func() {
		if p.inW != nil {
			p.closeError = p.inW.Close()
		}
		if p.outR != nil {
			_ = p.outR.Close()
		}
	})
	return p.closeError
}

func (p *terminalProcess) Kill() {
	if p.cmd != nil && p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *terminalProcess) IsPTY() bool { return p.pty }
