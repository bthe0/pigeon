package client

import (
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const daemonEnvKey = "PIGEON_DAEMON"

// IsDaemon returns true when the current process is running as the daemon.
func IsDaemon() bool {
	return os.Getenv(daemonEnvKey) == "1"
}

// DaemonStart forks the current binary as a background daemon.
func DaemonStart() error {
	pidFile, err := PIDFile()
	if err != nil {
		return err
	}

	if pid, err := readPID(pidFile); err == nil {
		if processRunning(pid) {
			return fmt.Errorf("daemon already running (PID %d)", pid)
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	// Build args: replace any "daemon start" with the underlying run args
	args := filterArgs(os.Args[1:], "daemon", "start")
	args = append([]string{"daemon", "run"}, args...)

	logDir, err := LogDir()
	if err != nil {
		return err
	}
	logPath := filepath.Join(logDir, "daemon.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonEnvKey+"=1")
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		f.Close()
		return fmt.Errorf("start daemon: %w", err)
	}
	f.Close()

	if err := writePID(pidFile, cmd.Process.Pid); err != nil {
		return err
	}

	fmt.Printf("Daemon started (PID %d)\nLogs: %s\n", cmd.Process.Pid, logPath)
	return nil
}

// DaemonStop kills the daemon process.
func DaemonStop() error {
	pidFile, err := PIDFile()
	if err != nil {
		return err
	}
	pid, err := readPID(pidFile)
	if err != nil {
		return fmt.Errorf("daemon not running")
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("kill: %w", err)
	}
	os.Remove(pidFile)
	fmt.Printf("Daemon stopped (PID %d)\n", pid)
	return nil
}

// DaemonStatus prints daemon status.
func DaemonStatus() {
	pidFile, _ := PIDFile()
	pid, err := readPID(pidFile)
	if err != nil || !processRunning(pid) {
		fmt.Println("Daemon: stopped")
		return
	}
	fmt.Printf("Daemon: running (PID %d)\n", pid)
}

// DaemonRun is the daemon's main loop — connects and reconnects with backoff.
func DaemonRun(cfg *Config) {
	pidFile, _ := PIDFile()
	writePID(pidFile, os.Getpid())
	defer os.Remove(pidFile)

	attempt := 0
	for {
		log.Printf("Connecting to %s (attempt %d)...", cfg.Server, attempt+1)
		c, err := New(cfg)
		if err != nil {
			log.Printf("client init: %v", err)
		} else {
			if err := c.Connect(); err != nil {
				log.Printf("disconnected: %v", err)
			}
			c.Close()
		}

		attempt++
		wait := time.Duration(math.Min(float64(30*time.Second), float64(time.Duration(attempt)*2*time.Second)))
		log.Printf("Reconnecting in %s...", wait)
		time.Sleep(wait)
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0644)
}

func readPID(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}

func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func filterArgs(args []string, remove ...string) []string {
	removeSet := make(map[string]bool)
	for _, r := range remove {
		removeSet[r] = true
	}
	var out []string
	for _, a := range args {
		if !removeSet[a] {
			out = append(out, a)
		}
	}
	return out
}
