// Package sftpclient manages reconnecting SSH and SFTP sessions.
package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"github.com/szok/synchro/internal/config"
	"golang.org/x/crypto/ssh"
)

const (
	reconnectDelay    = 3 * time.Second
	keepaliveInterval = 30 * time.Second
	dialLimit         = 10 * time.Second
)

var (
	// errShutdown is returned by connect when ctx was cancelled or Close was called.
	errShutdown = errors.New("synchro: shutdown")
	// errConnLost is returned by connect when a live connection dropped; the caller retries.
	errConnLost = errors.New("synchro: connection lost")
)

// Operation runs against a live SFTP client.
type Operation func(*sftp.Client) error

// Logger receives connection state changes and errors.
type Logger interface {
	Error(string)
	Connected(user, host string)
	Disconnected(retry time.Duration)
}

// Client executes operations after a connection becomes available.
type Client struct {
	config    config.Config
	log       Logger
	mu        sync.Mutex
	client    *sftp.Client
	queue     []Operation
	closed    bool
	ready     chan struct{}
	readyOnce sync.Once
}

// New constructs a connection manager. Call Start once to begin connecting.
func New(cfg config.Config, log Logger) *Client {
	return &Client{config: cfg, log: log, ready: make(chan struct{})}
}

// Start reconnects until ctx is cancelled. ready is closed after the first successful connection.
func (c *Client) Start(ctx context.Context) {
	go func() {
		for {
			if ctx.Err() != nil {
				c.Close()
				return
			}
			if err := c.connect(ctx); err != nil {
				if errors.Is(err, errShutdown) || ctx.Err() != nil {
					c.Close()
					return
				}
				if !errors.Is(err, errConnLost) {
					c.log.Error(fmt.Sprintf("SSH error: %v", err))
				}
				if !wait(ctx, reconnectDelay) {
					c.Close()
					return
				}
				continue
			}
			return
		}
	}()
}

func (c *Client) connect(ctx context.Context) error {
	sshConfig, err := BuildSSHConfig(c.config)
	if err != nil {
		return err
	}
	address := net.JoinHostPort(c.config.Host, strconv.Itoa(c.config.Port))
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "tcp", address)
	if err != nil {
		return err
	}
	connection, channels, requests, err := ssh.NewClientConn(conn, address, sshConfig)
	if err != nil {
		conn.Close()
		return err
	}
	sshClient := ssh.NewClient(connection, channels, requests)
	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return err
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		sftpClient.Close()
		sshClient.Close()
		return errShutdown
	}
	c.client = sftpClient
	queued := c.queue
	c.queue = nil
	c.mu.Unlock()
	c.readyOnce.Do(func() { close(c.ready) })
	c.log.Connected(c.config.Username, c.config.Host)
	for _, operation := range queued {
		if err := operation(sftpClient); err != nil {
			c.log.Error(fmt.Sprintf("SFTP operation error: %v", err))
		}
	}

	// Hold the transport open until ctx is cancelled or the connection drops.
	// ssh.Client.Wait unblocks when the underlying connection is gone; a periodic
	// keepalive request forces detection of a half-open link.
	lost := make(chan error, 1)
	go func() { lost <- sshClient.Wait() }()
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()

	detach := func() {
		c.mu.Lock()
		if c.client == sftpClient {
			c.client = nil
		}
		c.mu.Unlock()
		sftpClient.Close()
		sshClient.Close()
	}
	for {
		select {
		case <-ctx.Done():
			detach()
			return errShutdown
		case <-lost:
			detach()
			c.log.Disconnected(reconnectDelay)
			return errConnLost
		case <-ticker.C:
			if _, _, err := sshClient.SendRequest("keepalive@synchro", true, nil); err != nil {
				detach()
				c.log.Disconnected(reconnectDelay)
				return errConnLost
			}
		}
	}
}

// Run executes immediately when connected, otherwise queues the operation.
func (c *Client) Run(operation Operation) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	if c.client == nil {
		c.queue = append(c.queue, operation)
		c.mu.Unlock()
		return
	}
	client := c.client
	c.mu.Unlock()
	if err := operation(client); err != nil {
		c.log.Error(fmt.Sprintf("SFTP operation error: %v", err))
	}
}

// Ready closes once the first SFTP connection is usable.
func (c *Client) Ready() <-chan struct{} { return c.ready }

// Close prevents new work and releases the current SFTP connection.
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	c.queue = nil
	if c.client != nil {
		_ = c.client.Close()
		c.client = nil
	}
}

// TestConnection opens and closes one SSH/SFTP connection without retrying.
func TestConnection(cfg config.Config) error {
	_, closeClient, err := Dial(cfg)
	if err != nil {
		return err
	}
	closeClient()
	return nil
}

// Dial opens one SSH/SFTP connection without retrying; the handshake must
// finish within dialLimit. The returned function closes the connection.
func Dial(cfg config.Config) (*sftp.Client, func(), error) {
	sshConfig, err := BuildSSHConfig(cfg)
	if err != nil {
		return nil, nil, fmt.Errorf("build SSH configuration: %w", err)
	}
	address := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	conn, err := net.DialTimeout("tcp", address, dialLimit)
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", address, err)
	}
	if err := conn.SetDeadline(time.Now().Add(dialLimit)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("set connection deadline: %w", err)
	}

	connection, channels, requests, err := ssh.NewClientConn(conn, address, sshConfig)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("authenticate with %s: %w", address, err)
	}
	sshClient := ssh.NewClient(connection, channels, requests)
	if err := conn.SetDeadline(time.Time{}); err != nil {
		sshClient.Close()
		return nil, nil, fmt.Errorf("clear connection deadline: %w", err)
	}

	sftpClient, err := sftp.NewClient(sshClient)
	if err != nil {
		sshClient.Close()
		return nil, nil, fmt.Errorf("start SFTP session: %w", err)
	}
	return sftpClient, func() {
		sftpClient.Close()
		sshClient.Close()
	}, nil
}

func wait(ctx context.Context, delay time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(delay):
		return true
	}
}

// BuildSSHConfig translates the JSON auth configuration to Go's SSH client config.
func BuildSSHConfig(cfg config.Config) (*ssh.ClientConfig, error) {
	result := &ssh.ClientConfig{User: cfg.Username, HostKeyCallback: ssh.InsecureIgnoreHostKey()} // Compatibility with ssh2's permissive default; see README security note.
	switch cfg.Auth {
	case "none":
		return result, nil
	case "password":
		result.Auth = []ssh.AuthMethod{ssh.Password(cfg.Password)}
		return result, nil
	case "key":
		keyPath, err := config.ExpandHome(cfg.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		key, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("read private key %q: %w", keyPath, err)
		}
		var signer ssh.Signer
		if cfg.Password == "" {
			signer, err = ssh.ParsePrivateKey(key)
		} else {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(key, []byte(cfg.Password))
		}
		if err != nil {
			return nil, fmt.Errorf("parse private key %q: %w", keyPath, err)
		}
		result.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
		return result, nil
	default:
		return nil, fmt.Errorf("unsupported auth method %q", cfg.Auth)
	}
}
