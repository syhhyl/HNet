package mihomo

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"hnet/internal/app"
)

type runningProcess struct {
	cmd  *exec.Cmd
	done chan error
	log  *os.File
}

type Supervisor struct {
	paths       app.Paths
	mu          sync.Mutex
	proc        *runningProcess
	binaryPath  string
	lastWaitErr string
}

func NewSupervisor(paths app.Paths) *Supervisor {
	return &Supervisor{paths: paths}
}

func (s *Supervisor) Apply(controllerPort int, secret string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.stopLocked(); err != nil {
		return "", err
	}
	return s.startLocked(controllerPort, secret)
}

func (s *Supervisor) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopLocked()
}

func (s *Supervisor) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.proc != nil
}

func (s *Supervisor) startLocked(controllerPort int, secret string) (string, error) {
	binaryPath, err := FindBinary()
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(s.paths.MihomoLogPath), 0o755); err != nil {
		return "", err
	}

	logFile, err := os.OpenFile(s.paths.MihomoLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}

	cmd := exec.Command(binaryPath, "-d", s.paths.RuntimeDir, "-f", s.paths.MihomoConfigPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return "", fmt.Errorf("start mihomo: %w", err)
	}

	proc := &runningProcess{cmd: cmd, done: make(chan error, 1), log: logFile}
	s.proc = proc
	s.binaryPath = binaryPath
	go s.waitForExit(proc)

	if err := waitForController(controllerPort, secret); err != nil {
		_ = s.stopLocked()
		return "", err
	}

	return binaryPath, nil
}

func (s *Supervisor) waitForExit(proc *runningProcess) {
	err := proc.cmd.Wait()
	_ = proc.log.Close()
	proc.done <- err
	close(proc.done)

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc == proc {
		s.proc = nil
	}
	if err != nil {
		s.lastWaitErr = err.Error()
	} else {
		s.lastWaitErr = ""
	}
}

func (s *Supervisor) stopLocked() error {
	proc := s.proc
	if proc == nil {
		return nil
	}
	s.proc = nil

	if proc.cmd.Process == nil {
		return nil
	}

	_ = proc.cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-proc.done:
		return nil
	case <-time.After(5 * time.Second):
	}

	if err := proc.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	<-proc.done
	return nil
}

func FindBinary() (string, error) {
	if path := os.Getenv("HNET_MIHOMO_BIN"); path != "" {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}

	if path, err := exec.LookPath("mihomo"); err == nil {
		return path, nil
	}

	candidates := []string{
		"/opt/homebrew/bin/mihomo",
		"/usr/local/bin/mihomo",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}

	return "", errors.New("mihomo not found; install it first, for example: brew install mihomo")
}

func waitForController(controllerPort int, secret string) error {
	transport := &http.Transport{Proxy: nil}
	client := &http.Client{Timeout: 800 * time.Millisecond, Transport: transport}
	url := fmt.Sprintf("http://127.0.0.1:%d/version", controllerPort)
	deadline := time.Now().Add(20 * time.Second)

	for time.Now().Before(deadline) {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+secret)
			resp, err := client.Do(req)
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					return nil
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("mihomo controller did not become ready on 127.0.0.1:%d", controllerPort)
}
