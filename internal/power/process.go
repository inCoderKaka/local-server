package power

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// Runner starts the helper programs of the process-based adapters (caffeinate, systemd-inhibit).
// It is a port so tests never start a real one.
type Runner interface {
	// Start starts name with args and returns at once. A program that is not installed returns
	// an error wrapping ErrToolMissing.
	Start(name string, args ...string) (Process, error)
}

// Process is a started helper program.
type Process interface {
	// Stop kills the program and waits until it is gone. It is safe to call more than once.
	Stop() error
	// Done is closed when the program has exited, however it ended.
	Done() <-chan struct{}
}

// command is a helper program and its arguments.
type command struct {
	name string
	args []string
}

// commandFor builds the helper command for the local-server process pid.
type commandFor func(pid int) command

// darwinCommand holds the Mac awake for as long as pid lives: caffeinate -i prevents idle sleep
// and -w ends it when the process exits, so a crash of local-server never leaves it running.
func darwinCommand(pid int) command {
	return command{name: "caffeinate", args: []string{"-i", "-w", strconv.Itoa(pid)}}
}

// linuxCommand holds a logind sleep and idle inhibitor for as long as pid lives: the command
// systemd-inhibit runs while holding it is tail --pid, which exits when pid does.
func linuxCommand(pid int) command {
	return command{name: "systemd-inhibit", args: []string{
		"--what=sleep:idle", "--who=local-server", "--why=Sharing is on", "--mode=block",
		"tail", "--pid=" + strconv.Itoa(pid), "-f", "/dev/null",
	}}
}

// processPower is the Power of an OS whose sleep lock is a helper process.
type processPower struct {
	runner Runner
	cmd    commandFor
	pid    int
	log    *slog.Logger

	missing sync.Once
}

// newProcessPower returns a Power that runs cmd(pid) through runner; pid is the local-server
// process. A nil log discards.
func newProcessPower(r Runner, cmd commandFor, pid int, log *slog.Logger) *processPower {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &processPower{runner: r, cmd: cmd, pid: pid, log: log}
}

func (p *processPower) Acquire(context.Context) (Lock, error) {
	c := p.cmd(p.pid)
	proc, err := p.runner.Start(c.name, c.args...)
	if errors.Is(err, ErrToolMissing) {
		p.missing.Do(func() {
			p.log.Warn("sleep lock unavailable: the host will not be kept awake while sharing", "tool", c.name, "error", err)
		})
		return noopLock{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("power: start %s: %w", c.name, err)
	}
	return &processLock{proc: proc}, nil
}

// processLock holds the lock for as long as its helper process runs.
type processLock struct {
	proc Process
	once sync.Once
	err  error
}

func (l *processLock) Release() error {
	l.once.Do(func() { l.err = l.proc.Stop() })
	return l.err
}

func (l *processLock) Alive() bool {
	select {
	case <-l.proc.Done():
		return false
	default:
		return true
	}
}

// ExecRunner is the Runner that starts real programs with os/exec.
type ExecRunner struct{}

// Start implements Runner.
func (ExecRunner) Start(name string, args ...string) (Process, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return nil, fmt.Errorf("%w: %s", ErrToolMissing, name)
		}
		return nil, err
	}
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &execProcess{cmd: cmd, done: make(chan struct{})}
	go func() {
		_ = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

type execProcess struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (p *execProcess) Done() <-chan struct{} { return p.done }

func (p *execProcess) Stop() error {
	if err := p.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		select {
		case <-p.done: // it exited on its own between the check and the kill
		default:
			return err
		}
	}
	<-p.done
	return nil
}
