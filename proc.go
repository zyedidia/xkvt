package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/zyedidia/xkvt/ptrace"
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
	OnRead   func(path string)
	OnWrite  func(path string)
	OnRemove func(path string)
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
		unix.PTRACE_O_TRACESYSGOOD | unix.PTRACE_O_TRACEEXIT | unix.PTRACE_O_TRACEEXEC

	p := newTracedProc(cmd.Process.Pid, opts)
	p.tracer.SetOptions(options)
	err = p.cont(0, false)

	return p, err

}

// Begins tracing an already existing process
func newTracedProc(pid int, opts Options) *Proc {
	p := &Proc{
		tracer: ptrace.NewTracer(pid),
		stack:  NewStack(),
		fds: map[int]string{
			0: "/dev/stdin",
			1: "/dev/stdout",
			2: "/dev/stderr",
		},
		opts: opts,
	}

	return p
}

func newForkedProc(from *Proc, cloneFiles bool, pid int, opts Options) *Proc {
	p := &Proc{
		tracer: ptrace.NewTracer(pid),
		stack:  NewStack(),
		opts:   opts,
	}

	if cloneFiles {
		p.fds = from.fds
	} else {
		p.fds = make(map[int]string)
		for k, v := range from.fds {
			p.fds[k] = v
		}
	}

	return p
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
		var flags uint64
		var err error
		switch regs.Orig_rax {
		case unix.SYS_OPEN:
			path, err = p.tracer.ReadCString(uintptr(regs.Rdi))
			flags = regs.Rsi
		case unix.SYS_OPENAT:
			path, err = p.tracer.ReadCString(uintptr(regs.Rsi))
			flags = regs.Rdx
		}

		if err != nil {
			return nil, err
		}

		var wd string
		if regs.Orig_rax != unix.SYS_OPENAT || int32(regs.Rdi) == unix.AT_FDCWD {
			wd, err = p.Wd()
			if err != nil {
				return nil, err
			}
		} else {
			wd = p.fds[int(regs.Rdi)]
		}
		path = abs(path, wd)

		return func() error {
			var exitRegs unix.PtraceRegs
			p.tracer.GetRegs(&exitRegs)
			fd := int(exitRegs.Rax)
			if fd < 0 {
				return nil
			}
			if (flags&0b11) == unix.O_WRONLY || (flags&0b11) == unix.O_RDWR {
				p.opts.OnWrite(path)
			} else if (flags & 0b11) == unix.O_RDONLY {
				p.opts.OnRead(path)
			}
			p.fds[fd] = path
			return nil
		}, nil
	case unix.SYS_RENAMEAT:
		var newdir string
		if int32(regs.Rdx) == unix.AT_FDCWD {
			var err error
			newdir, err = p.Wd()
			if err != nil {
				return nil, err
			}
		} else {
			newdir = p.fds[int(regs.Rdx)]
		}
		var olddir string
		if int32(regs.Rdi) == unix.AT_FDCWD {
			var err error
			olddir, err = p.Wd()
			if err != nil {
				return nil, err
			}
		} else {
			olddir = p.fds[int(regs.Rdi)]
		}
		oldpath, err := p.tracer.ReadCString(uintptr(regs.Rsi))
		if err != nil {
			return nil, err
		}
		newpath, err := p.tracer.ReadCString(uintptr(regs.R10))
		if err != nil {
			return nil, err
		}
		p.opts.OnWrite(abs(newpath, newdir))
		p.opts.OnRemove(abs(oldpath, olddir))
	case unix.SYS_RENAME:
		wd, err := p.Wd()
		if err != nil {
			return nil, err
		}
		oldpath, err := p.tracer.ReadCString(uintptr(regs.Rdi))
		if err != nil {
			return nil, err
		}
		newpath, err := p.tracer.ReadCString(uintptr(regs.Rsi))
		if err != nil {
			return nil, err
		}
		p.opts.OnWrite(abs(newpath, wd))
		p.opts.OnRemove(abs(oldpath, wd))
	case unix.SYS_UNLINK:
		wd, err := p.Wd()
		if err != nil {
			return nil, err
		}
		path, err := p.tracer.ReadCString(uintptr(regs.Rdi))
		if err != nil {
			return nil, err
		}
		p.opts.OnRemove(abs(path, wd))
		// A previous version of xkvt used the read/write syscalls to determine
		// how files are accessed, but this proved too brittle because it makes
		// it difficult to account for things like pipes and mmap.
		// case unix.SYS_WRITE, unix.SYS_READ:
		// 	wd, err := p.Wd()
		// 	if err != nil {
		// 		return nil, err
		// 	}
		// 	filename, ok := p.fds[int(regs.Rdi)]
		// 	if !ok {
		// 		log.Println(regs.Rdi, "not found")
		// 		return nil, nil
		// 	}
		// 	if regs.Orig_rax == unix.SYS_WRITE {
		// 		p.opts.OnWrite(abs(filename, wd))
		// 	} else {
		// 		p.opts.OnRead(abs(filename, wd))
		// 	}
	}
	return nil, nil
}

func (p *Proc) Wd() (string, error) {
	return os.Readlink(fmt.Sprintf("/proc/%d/cwd", p.Pid()))
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

func abs(path, wd string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(wd, path)
}
