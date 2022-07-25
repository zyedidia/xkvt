package main

import (
	"os"
	"os/exec"

	"github.com/zyedidia/bld/ptrace"
	"golang.org/x/sys/unix"
)

type ProcState int

type ExitFunc func() error

const (
	PSysEnter ProcState = iota
	PSysExit
)

type Proc struct {
	tracer *ptrace.Tracer
	state  ProcState
	exited bool
	stack  *FuncStack
	fds    map[int]string
	opts   Options
}

type Options struct {
	OnRead  func(path string)
	OnWrite func(path string)
}

func startProc(target string, args []string, opts Options) (*Proc, error) {
	cmd := exec.Command(target, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.SysProcAttr = &unix.SysProcAttr{
		Ptrace: true,
	}

	err := cmd.Start()
	if err != nil {
		return nil, err
	}
	// wait for execve
	cmd.Wait()

	options := unix.PTRACE_O_EXITKILL | unix.PTRACE_O_TRACECLONE |
		unix.PTRACE_O_TRACEFORK | unix.PTRACE_O_TRACEVFORK |
		unix.PTRACE_O_TRACESYSGOOD | unix.PTRACE_O_TRACEEXIT

	p, err := newTracedProc(cmd.Process.Pid, opts)
	if err != nil {
		return nil, err
	}
	p.tracer.SetOptions(options)
	// err = p.tracer.ReAttachAndContinue(options)
	// if err != nil {
	// 	return nil, err
	// }
	//
	// // Wait for the initial SIGTRAP created because we are attaching
	// // with ReAttachAndContinue to properly handle group stops.
	// var ws unix.WaitStatus
	// _, err = unix.Wait4(p.tracer.Pid(), &ws, 0, nil)
	// if err != nil {
	// 	return nil, err
	// 	// } else if ws.StopSignal() != unix.SIGTRAP {
	// 	// 	return nil, errors.New("wait: received non SIGTRAP: " + ws.StopSignal().String())
	// }
	err = p.cont(0, false)

	return p, err

}

// Begins tracing an already existing process
func newTracedProc(pid int, opts Options) (*Proc, error) {
	p := &Proc{
		tracer: ptrace.NewTracer(pid),
		stack:  NewStack(),
		fds: map[int]string{
			0: "stdin",
			1: "stdout",
			2: "stderr",
		},
		opts: opts,
	}

	return p, nil
}

func (p *Proc) handleInterrupt() error {
	switch p.state {
	case PSysEnter:
		p.state = PSysExit
		f, err := p.syscallEnter()
		if err != nil || f == nil {
			return err
		}
		p.stack.Push(f)
	case PSysExit:
		p.state = PSysEnter
		var f ExitFunc
		if p.stack.Size() > 0 {
			f = p.stack.Pop()
		}
		if f != nil {
			return f()
		}
	}
	return nil
}

func (p *Proc) syscallEnter() (ExitFunc, error) {
	var regs unix.PtraceRegs
	p.tracer.GetRegs(&regs)

	switch regs.Orig_rax {
	case unix.SYS_CLOSE:
		fd := int(regs.Rdi)
		if _, ok := p.fds[fd]; ok {
			delete(p.fds, fd)
			return nil, nil
		}
	case unix.SYS_OPEN, unix.SYS_OPENAT:
		var path string
		var err error
		switch regs.Orig_rax {
		case unix.SYS_OPEN:
			path, err = p.tracer.ReadCString(uintptr(regs.Rdi))
		case unix.SYS_OPENAT:
			path, err = p.tracer.ReadCString(uintptr(regs.Rsi))
		}
		if err != nil {
			return nil, err
		}
		return func() error {
			var exitRegs unix.PtraceRegs
			p.tracer.GetRegs(&exitRegs)
			fd := int(exitRegs.Rax)
			if fd < 0 {
				return nil
			}
			p.fds[fd] = path
			return nil
		}, nil

	case unix.SYS_WRITE, unix.SYS_READ:
		filename := p.fds[int(regs.Rdi)]
		if regs.Orig_rax == unix.SYS_WRITE {
			p.opts.OnWrite(filename)
		} else {
			p.opts.OnRead(filename)
		}
	}
	return nil, nil
}

func (p *Proc) exit() {
	p.exited = true
}

func (p *Proc) Exited() bool {
	return p.exited
}

func (p *Proc) cont(sig unix.Signal, groupStop bool) error {
	if groupStop {
		return p.tracer.Listen()
	}
	return p.tracer.Syscall(sig)
}

func (p *Proc) Pid() int {
	return p.tracer.Pid()
}
