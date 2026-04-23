package client

import (
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

const daemonEnvKey = "PIGEON_DAEMON"

// Tunneling pause/resume state shared between the web API handlers and
// DaemonRun's connection loop. Paused is true when the user has stopped
// tunneling from the dashboard; the loop then blocks on daemonResume instead
// of reconnecting. currentClient holds the live connection (if any) so
// DaemonPause can cut it and force the loop to re-evaluate paused state.
var (
	daemonPaused  atomic.Bool
	daemonResume  = make(chan struct{}, 1)
	currentClient atomic.Pointer[Client]
)

// DaemonIsPaused reports whether tunneling is currently paused.
func DaemonIsPaused() bool {
	return daemonPaused.Load()
}

// DaemonPause stops the tunneling loop without killing the daemon process.
// The web UI stays reachable. The current client connection (if any) is
// closed so Connect returns and the loop falls through to the paused wait.
func DaemonPause() {
	daemonPaused.Store(true)
	if c := currentClient.Load(); c != nil {
		c.Close()
	}
}

// DaemonResume clears the paused flag and wakes the loop's resume wait.
func DaemonResume() {
	daemonPaused.Store(false)
	select {
	case daemonResume <- struct{}{}:
	default:
	}
}

// IsDaemon returns true when the current process is running as the daemon.
func IsDaemon() bool {
	return os.Getenv(daemonEnvKey) == "1"
}

// DaemonStart forks the current binary as a background daemon. Uses
// O_CREATE|O_EXCL on the PID file as a lock: if two processes race, only
// one wins the open and the other returns "already running".
func DaemonStart() error {
	pidFile, err := PIDFile()
	if err != nil {
		return err
	}

	pf, err := acquirePIDFile(pidFile)
	if err != nil {
		return err
	}
	// At this point we own pf. On any error below we must remove the pid file.
	cleanup := func() { pf.Close(); os.Remove(pidFile) }

	exe, err := os.Executable()
	if err != nil {
		cleanup()
		return err
	}

	// Build args: replace any "daemon start" with the underlying run args
	args := filterArgs(os.Args[1:], "daemon", "start")
	args = append([]string{"daemon", "run"}, args...)

	logDir, err := LogDir()
	if err != nil {
		cleanup()
		return err
	}
	logPath := filepath.Join(logDir, "daemon.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		cleanup()
		return err
	}

	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), daemonEnvKey+"=1")
	cmd.Stdout = f
	cmd.Stderr = f
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		f.Close()
		cleanup()
		return fmt.Errorf("start daemon: %w", err)
	}
	f.Close()

	// Overwrite the parent PID we stamped during acquirePIDFile with the
	// child's real PID. Seek+Truncate so the new content fully replaces the
	// old rather than getting appended.
	if _, err := pf.Seek(0, 0); err != nil {
		pf.Close()
		os.Remove(pidFile)
		return err
	}
	if err := pf.Truncate(0); err != nil {
		pf.Close()
		os.Remove(pidFile)
		return err
	}
	if _, err := fmt.Fprintf(pf, "%d", cmd.Process.Pid); err != nil {
		pf.Close()
		os.Remove(pidFile)
		return err
	}
	pf.Close()

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

// DaemonReload sends SIGHUP to the daemon, triggering an immediate reconnect.
func DaemonReload() {
	pidFile, err := PIDFile()
	if err != nil {
		return
	}
	pid, err := readPID(pidFile)
	if err != nil {
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	_ = proc.Signal(syscall.SIGHUP)
}

// DaemonRun is the daemon's main loop — connects and reconnects with backoff.
func DaemonRun(cfg *Config) {
	pidFile, _ := PIDFile()
	writePID(pidFile, os.Getpid())
	defer os.Remove(pidFile)

	// Tee the default logger to daemon.log so the System Logs view in the
	// dashboard has content even when the daemon is started inline (pigeon dev)
	// where stdout/stderr are bound to the terminal rather than the log file.
	if logDir, err := LogDir(); err == nil {
		if f, err := os.OpenFile(filepath.Join(logDir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600); err == nil {
			log.SetOutput(io.MultiWriter(os.Stderr, f))
		}
	}

	// Start web interface in background
	go func() {
		addr := cfg.WebAddr
		if addr == "" {
			addr = "127.0.0.1:8080"
		}
		log.Printf("Web interface starting on %s", addr)
		log.Printf("Access the dashboard at: http://%s", addr)
		if err := StartWebInterface(addr, false); err != nil {
			log.Printf("Web interface failed: %v", err)
		}
	}()

	reload := make(chan struct{}, 1)
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGHUP)
	go func() {
		for range sigs {
			select {
			case reload <- struct{}{}:
			default:
			}
		}
	}()

	attempt := 0
	for {
		// If the user stopped tunneling from the dashboard, block here until
		// they resume (or until a config reload nudges us — config changes
		// are pointless while paused but we still want to exit promptly if
		// resume happens via a fresh reload signal).
		if daemonPaused.Load() {
			log.Printf("Tunneling paused — waiting for resume")
			select {
			case <-daemonResume:
				log.Printf("Tunneling resumed")
				attempt = 0
			case <-reload:
				// Reload while paused: stay paused, just loop to re-check.
				continue
			}
		}

		// Always reload config from disk so edits made via web UI are picked up.
		if fresh, err := LoadConfig(); err == nil {
			cfg = fresh
		}

		log.Printf("Connecting to %s (attempt %d)...", cfg.Server, attempt+1)
		c, err := New(cfg)
		if err != nil {
			log.Printf("client init: %v", err)
		} else {
			c.OnAddr = func(id, publicAddr string) {
				changed := false
				for i := range cfg.Forwards {
					if cfg.Forwards[i].ID == id && cfg.Forwards[i].PublicAddr != publicAddr {
						cfg.Forwards[i].PublicAddr = publicAddr
						changed = true
						break
					}
				}
				if changed {
					if err := SaveConfig(cfg); err != nil {
						log.Printf("save config: %v", err)
					}
				}
			}
			currentClient.Store(c)
			done := make(chan struct{})
			go func() {
				if err := c.Connect(); err != nil {
					log.Printf("disconnected: %v", err)
				}
				c.Close()
				close(done)
			}()
			select {
			case <-done:
				currentClient.Store(nil)
			case <-reload:
				c.Close()
				currentClient.Store(nil)
				// Small delay to ensure Web UI has finished saving config.json
				time.Sleep(100 * time.Millisecond)
				log.Printf("Config reloaded — reconnecting...")
				attempt = 0
				continue
			}
		}

		// If DaemonPause closed the client above, skip the backoff and jump
		// back to the paused wait at the top of the loop.
		if daemonPaused.Load() {
			continue
		}

		attempt++
		wait := time.Duration(math.Min(float64(30*time.Second), float64(time.Duration(attempt)*2*time.Second)))
		log.Printf("Reconnecting in %s...", wait)

		select {
		case <-time.After(wait):
		case <-reload:
			log.Printf("Config reloaded — reconnecting...")
			attempt = 0
		}
	}
}

// ── Helpers ────────────────────────────────────────────────────────────────────

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(strconv.Itoa(pid)), 0600)
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

// acquirePIDFile opens path with O_CREATE|O_EXCL as a mutual-exclusion lock
// and stamps our own PID into it immediately. Stamping is what makes the
// exclusion usable under races: a concurrent caller that finds the file
// already exists will see a valid running PID (ours) rather than an empty
// file it could otherwise treat as stale.
//
// Stale files (owner no longer running, or malformed) are removed and the
// open retried exactly once. Callers later overwrite our parent PID with the
// child's PID via writePID, which truncates before writing.
func acquirePIDFile(path string) (*os.File, error) {
	for attempt := 0; attempt < 2; attempt++ {
		pf, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			if _, werr := fmt.Fprintf(pf, "%d", os.Getpid()); werr != nil {
				pf.Close()
				os.Remove(path)
				return nil, werr
			}
			return pf, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if attempt > 0 {
			return nil, fmt.Errorf("acquire pid file %s: %w", path, err)
		}
		// PID file exists. If the owner is still running, refuse to take over.
		if existing, rerr := readPID(path); rerr == nil && processRunning(existing) {
			return nil, fmt.Errorf("daemon already running (PID %d)", existing)
		}
		if rmErr := os.Remove(path); rmErr != nil {
			return nil, fmt.Errorf("stale pid file %s: %w", path, rmErr)
		}
	}
	// Unreachable under the two-attempt loop.
	return nil, fmt.Errorf("acquire pid file %s", path)
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
