package main

import (
	"errors"
	"log"

	"golang.org/x/sys/unix"
)

var ErrFinishedTrace = errors.New("tracing finished")

type Status struct {
	unix.WaitStatus

	sig       unix.Signal
	groupStop bool
}

type Program struct {
	procs map[int]*Proc
	opts  Options
}

func NewProgram(target string, args []string, opts Options) (*Program, int, error) {
	prog := new(Program)
	prog.opts = opts

	proc, err := startProc(target, args, opts)
	if err != nil {
		return nil, 0, err
	}

	prog.procs = map[int]*Proc{
		proc.Pid(): proc,
	}

	return prog, proc.Pid(), err
}

func (p *Program) Wait(status *Status) (*Proc, error) {
	ws := &status.WaitStatus

	wpid, err := unix.Wait4(-1, ws, 0, nil)
	if err != nil {
		return nil, err
	}

	status.sig = 0
	status.groupStop = false
	proc, ok := p.procs[wpid]
	if !ok {
		proc = newTracedProc(wpid, p.opts)
		p.procs[wpid] = proc
		log.Printf("%d: new process created (tracing enabled)\n", wpid)
		return proc, nil
	}

	if ws.Exited() || ws.Signaled() {
		log.Printf("%d: exited\n", wpid)
		delete(p.procs, wpid)
		proc.exit()

		if len(p.procs) == 0 {
			return proc, ErrFinishedTrace
		}
	} else if !ws.Stopped() {
		log.Printf("%d: not stopped?\n", wpid)
		return proc, nil
	} else if ws.StopSignal() == (unix.SIGTRAP | 0x80) {
		proc.handleInterrupt()
	} else if ws.StopSignal() != unix.SIGTRAP {
		if statusPtraceEventStop(*ws) {
			status.groupStop = true
			log.Printf("%d: received group stop\n", wpid)
		} else {
			log.Printf("%d: received signal '%s'\n", wpid, ws.StopSignal())
			status.sig = ws.StopSignal()
		}
	} else if ws.TrapCause() == unix.PTRACE_EVENT_CLONE {
		newpid, err := proc.tracer.GetEventMsg()
		if err != nil {
			return proc, err
		}
		log.Printf("%d: called clone() = %d (err=%v)\n", wpid, newpid, err)
		child := newForkedProc(proc, false, int(newpid), p.opts)
		p.procs[child.Pid()] = child
		log.Printf("%d: new process cloned (tracing enabled)\n", child.Pid())
	} else if ws.TrapCause() == unix.PTRACE_EVENT_FORK {
		newpid, err := proc.tracer.GetEventMsg()
		if err != nil {
			return proc, err
		}
		log.Printf("%d: called fork()\n", wpid)
		child := newForkedProc(proc, false, int(newpid), p.opts)
		p.procs[child.Pid()] = child
		log.Printf("%d: new process forked (tracing enabled)\n", child.Pid())
	} else if ws.TrapCause() == unix.PTRACE_EVENT_VFORK {
		newpid, err := proc.tracer.GetEventMsg()
		if err != nil {
			return proc, err
		}
		log.Printf("%d: called vfork()\n", wpid)
		child := newForkedProc(proc, false, int(newpid), p.opts)
		p.procs[child.Pid()] = child
		log.Printf("%d: new process vforked (tracing enabled)\n", child.Pid())
	} else if ws.TrapCause() == unix.PTRACE_EVENT_EXEC {
		log.Printf("%d: called execve()\n", wpid)
	} else if ws.TrapCause() == unix.PTRACE_EVENT_EXIT {
		log.Printf("%d: called exit()\n", wpid)
	} else {
		log.Printf("%d: trapped, continuing\n", wpid)
	}
	return proc, nil
}

// Continue resumes execution of the given process. The wait status must be
// passed to replay any signals that were received while waiting.
func (p *Program) Continue(pr *Proc, status Status) error {
	return pr.cont(status.sig, status.groupStop)
}

func statusPtraceEventStop(status unix.WaitStatus) bool {
	return int(status)>>16 == unix.PTRACE_EVENT_STOP
}
