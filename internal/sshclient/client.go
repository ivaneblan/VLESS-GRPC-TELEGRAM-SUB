package sshclient

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/ivaneblan/vless-grpc-telegram-sub/internal/config"
	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

const defaultUser = "root"

type Client struct {
	conn     *ssh.Client
	host     string
	sec      *config.Secrets
	serverID string
}

// Reconnect re-establishes the SSH connection using the original credentials.
// Useful after an action (e.g. restarting an exit's xray) severs the session
// because the management traffic itself is routed through that node.
func (c *Client) Reconnect() error {
	if c.sec == nil {
		return fmt.Errorf("reconnect %s: no stored credentials", c.host)
	}
	nc, err := Connect(c.host, c.sec, c.serverID)
	if err != nil {
		return err
	}
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.conn = nc.conn
	return nil
}

func Connect(host string, sec *config.Secrets, serverID string) (*Client, error) {
	var auth []ssh.AuthMethod
	if key, err := parsePrivateKey(sec.SSH.PrivateKey); err == nil && key != nil {
		auth = append(auth, ssh.PublicKeys(key))
	}
	password := sec.Password(serverID)
	if password != "" {
		auth = append(auth, ssh.Password(password))
	}
	if len(auth) == 0 {
		if env := os.Getenv("XRAY_ROOT_PASSWORD"); env != "" {
			auth = append(auth, ssh.Password(env))
		}
	}
	if len(auth) == 0 {
		return nil, fmt.Errorf("no SSH auth for %s", host)
	}

	cfg := &ssh.ClientConfig{
		User:            defaultUser,
		Auth:            auth,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	addr := host + ":22"
	tcpConn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	// Timeout on ClientConfig covers TCP only; cap full handshake (incl. auth).
	_ = tcpConn.SetDeadline(time.Now().Add(30 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, cfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", host, err)
	}
	_ = tcpConn.SetDeadline(time.Time{})
	return &Client{conn: ssh.NewClient(sshConn, chans, reqs), host: host, sec: sec, serverID: serverID}, nil
}

// ConnectPassword connects using root password only (used to verify password changes).
func ConnectPassword(host, password string) (*Client, error) {
	password = strings.TrimSpace(password)
	if password == "" {
		return nil, fmt.Errorf("empty password for %s", host)
	}
	cfg := &ssh.ClientConfig{
		User:            defaultUser,
		Auth:            []ssh.AuthMethod{ssh.Password(password)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	addr := host + ":22"
	tcpConn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", host, err)
	}
	_ = tcpConn.SetDeadline(time.Now().Add(30 * time.Second))
	sshConn, chans, reqs, err := ssh.NewClientConn(tcpConn, addr, cfg)
	if err != nil {
		_ = tcpConn.Close()
		return nil, fmt.Errorf("ssh handshake %s: %w", host, err)
	}
	_ = tcpConn.SetDeadline(time.Time{})
	return &Client{conn: ssh.NewClient(sshConn, chans, reqs)}, nil
}

func SetRootPassword(c *Client, password string) error {
	script := fmt.Sprintf(`python3 - <<'PY'
import subprocess
p = %q
subprocess.run(["chpasswd"], input=f"root:{p}\n", text=True, check=True)
print("ok")
PY`, password)
	rc, out, errStr := c.RunScript(script, 30*time.Second)
	if rc != 0 || !strings.Contains(out, "ok") {
		return fmt.Errorf("chpasswd failed: %s %s", out, errStr)
	}
	return nil
}

func parsePrivateKey(pem string) (ssh.Signer, error) {
	pem = strings.TrimSpace(pem)
	if pem == "" {
		return nil, fmt.Errorf("empty key")
	}
	signer, err := ssh.ParsePrivateKey([]byte(pem))
	if err != nil {
		return nil, err
	}
	return signer, nil
}

func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

func (c *Client) RunScript(script string, timeout time.Duration) (int, string, string) {
	return c.runScript(script, timeout, false)
}

// RunScriptLive runs bash -s and streams stdout to os.Stdout while executing.
func (c *Client) RunScriptLive(script string, timeout time.Duration) (int, string, string) {
	return c.runScript(script, timeout, true)
}

func (c *Client) runScript(script string, timeout time.Duration, live bool) (int, string, string) {
	if timeout == 0 {
		timeout = 180 * time.Second
	}
	sess, err := c.conn.NewSession()
	if err != nil {
		return 1, "", err.Error()
	}
	defer sess.Close()

	stdoutPipe, err := sess.StdoutPipe()
	if err != nil {
		return 1, "", err.Error()
	}
	stderrPipe, err := sess.StderrPipe()
	if err != nil {
		return 1, "", err.Error()
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return 1, "", err.Error()
	}

	var stdout, stderr bytes.Buffer
	var stdoutW io.Writer = &stdout
	var stderrW io.Writer = &stderr
	if live {
		stdoutW = io.MultiWriter(os.Stdout, &stdout)
		stderrW = io.MultiWriter(os.Stderr, &stderr)
	}

	pipesDone := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(stdoutW, stdoutPipe)
		pipesDone <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(stderrW, stderrPipe)
		pipesDone <- struct{}{}
	}()

	shell := "bash -s"
	if live {
		shell = "stdbuf -oL -eL bash -s"
	}
	if err := sess.Start(shell); err != nil {
		return 1, "", err.Error()
	}
	_, _ = io.WriteString(stdin, script)
	_ = stdin.Close()

	done := make(chan error, 1)
	go func() { done <- sess.Wait() }()

	select {
	case err := <-done:
		<-pipesDone
		<-pipesDone
		rc := 0
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				rc = exitErr.ExitStatus()
			} else {
				rc = 1
				stderr.WriteString(err.Error())
			}
		}
		return rc, stdout.String(), stderr.String()
	case <-time.After(timeout):
		_ = sess.Close()
		<-pipesDone
		<-pipesDone
		return 1, stdout.String(), "timeout"
	}
}

func (c *Client) Run(cmd string, timeout time.Duration) (int, string, string) {
	if timeout == 0 {
		timeout = 180 * time.Second
	}
	sess, err := c.conn.NewSession()
	if err != nil {
		return 1, "", err.Error()
	}
	defer sess.Close()

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr
	done := make(chan error, 1)
	go func() {
		done <- sess.Run(cmd)
	}()
	select {
	case err := <-done:
		rc := 0
		if err != nil {
			if exitErr, ok := err.(*ssh.ExitError); ok {
				rc = exitErr.ExitStatus()
			} else {
				rc = 1
				stderr.WriteString(err.Error())
			}
		}
		return rc, stdout.String(), stderr.String()
	case <-time.After(timeout):
		_ = sess.Close()
		return 1, stdout.String(), "timeout"
	}
}

func (c *Client) SFTP() (*sftp.Client, error) {
	return sftp.NewClient(c.conn)
}

func InstallAuthorizedKey(c *Client, pubkey string) (string, error) {
	pubkey = strings.TrimSpace(pubkey)
	escaped := strings.ReplaceAll(pubkey, "'", "'\"'\"'")
	script := fmt.Sprintf(
		"mkdir -p ~/.ssh && chmod 700 ~/.ssh && "+
			"touch ~/.ssh/authorized_keys && chmod 600 ~/.ssh/authorized_keys && "+
			"if grep -qxF '%s' ~/.ssh/authorized_keys 2>/dev/null; then "+
			"echo already; "+
			"else echo '%s' >> ~/.ssh/authorized_keys && echo added; fi",
		escaped, escaped,
	)
	rc, out, err := c.RunScript(script, 30*time.Second)
	if rc != 0 {
		return "", fmt.Errorf("install key failed: %s %s", out, err)
	}
	out = strings.TrimSpace(out)
	if strings.Contains(out, "already") {
		return "already", nil
	}
	return "added", nil
}

func writeRemoteFile(sftpClient *sftp.Client, remotePath string, data []byte, mode os.FileMode) error {
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if dir != "" {
		_ = sftpClient.MkdirAll(dir)
	}
	f, err := sftpClient.Create(remotePath)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return err
	}
	return f.Chmod(mode)
}

func UploadFile(c *Client, localPath, remotePath string) error {
	sftpClient, err := c.SFTP()
	if err != nil {
		return err
	}
	defer sftpClient.Close()
	data, err := os.ReadFile(localPath)
	if err != nil {
		return err
	}
	return writeRemoteFile(sftpClient, remotePath, data, 0o644)
}

func UploadBytes(c *Client, remotePath string, data []byte) error {
	sftpClient, err := c.SFTP()
	if err != nil {
		return err
	}
	defer sftpClient.Close()
	return writeRemoteFile(sftpClient, remotePath, data, 0o644)
}

func MkdirRemote(c *Client, path string) error {
	sftpClient, err := c.SFTP()
	if err != nil {
		return err
	}
	defer sftpClient.Close()
	return sftpClient.MkdirAll(path)
}

func DownloadBytes(c *Client, remotePath string) ([]byte, error) {
	sftpClient, err := c.SFTP()
	if err != nil {
		return nil, err
	}
	defer sftpClient.Close()
	f, err := sftpClient.Open(remotePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
