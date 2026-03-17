package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"hnet/internal/app"
	"hnet/internal/daemon"
)

func main() {
	paths, err := app.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve paths: %v\n", err)
		os.Exit(1)
	}

	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	switch command {
	case "serve":
		if err := runServe(paths); err != nil {
			fmt.Fprintf(os.Stderr, "hnetd serve: %v\n", err)
			os.Exit(1)
		}
	case "start":
		if err := startDetached(paths); err != nil {
			fmt.Fprintf(os.Stderr, "hnetd start: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("hnetd started, socket: %s\n", paths.SocketPath)
	case "stop":
		if err := stopDetached(paths); err != nil {
			fmt.Fprintf(os.Stderr, "hnetd stop: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("hnetd stopped")
	case "restart":
		if err := restartDetached(paths); err != nil {
			fmt.Fprintf(os.Stderr, "hnetd restart: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("hnetd restarted, socket: %s\n", paths.SocketPath)
	case "status":
		if err := printStatus(paths); err != nil {
			fmt.Fprintf(os.Stderr, "hnetd status: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "usage: %s [serve|start|stop|restart|status]\n", filepath.Base(os.Args[0]))
		os.Exit(2)
	}
}

func runServe(paths app.Paths) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	svc, err := daemon.NewService(paths)
	if err != nil {
		return err
	}

	return svc.Serve(ctx)
}

func startDetached(paths app.Paths) error {
	if ok, _ := socketAlive(paths.SocketPath); ok {
		return errors.New("daemon is already running")
	}

	if pid, ok := readPID(paths.PIDFile); ok && processAlive(pid) {
		return errors.New("daemon process is already running")
	}

	if err := os.MkdirAll(paths.BaseDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(paths.BaseDir, 0o700); err != nil {
		return err
	}

	logFile, err := os.OpenFile(paths.DaemonLogPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()

	executable, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(executable, "serve")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	if err := cmd.Start(); err != nil {
		return err
	}

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if ok, _ := socketAlive(paths.SocketPath); ok {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return errors.New("daemon did not become ready in time; check hnetd.log")
}

func stopDetached(paths app.Paths) error {
	pid, ok := readPID(paths.PIDFile)
	if !ok {
		if ok, _ := socketAlive(paths.SocketPath); ok {
			return errors.New("socket exists but pid file is missing; remove stale files or stop manually")
		}
		return errors.New("daemon is not running")
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !processAlive(pid) {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}

	return errors.New("daemon did not exit in time")
}

func restartDetached(paths app.Paths) error {
	if ok, _ := socketAlive(paths.SocketPath); ok {
		if err := stopDetached(paths); err != nil {
			return err
		}
	} else if pid, ok := readPID(paths.PIDFile); ok && processAlive(pid) {
		if err := stopDetached(paths); err != nil {
			return err
		}
	}

	return startDetached(paths)
}

func printStatus(paths app.Paths) error {
	if ok, _ := socketAlive(paths.SocketPath); ok {
		fmt.Printf("running\nsocket: %s\npid: %s\n", paths.SocketPath, paths.PIDFile)
		return nil
	}

	if pid, ok := readPID(paths.PIDFile); ok && processAlive(pid) {
		fmt.Printf("starting\npid: %d\n", pid)
		return nil
	}

	fmt.Println("stopped")
	return nil
}

func socketAlive(socketPath string) (bool, error) {
	conn, err := net.DialTimeout("unix", socketPath, 300*time.Millisecond)
	if err != nil {
		return false, nil
	}
	_ = conn.Close()
	return true, nil
}

func readPID(path string) (int, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, true
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}
