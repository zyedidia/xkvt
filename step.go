package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime"
	"strings"
)

type Excavation struct {
	Commands []Command
}

func (e *Excavation) ToJson() ([]byte, error) {
	return json.Marshal(e)
}

func (e *Excavation) ToKnit() []byte {
	buf := &bytes.Buffer{}
	buf.WriteString("return r{\n")
	for _, c := range e.Commands {
		buf.Write(c.ToKnit())
		buf.WriteByte('\n')
	}
	buf.WriteString("}")
	return buf.Bytes()
}

type Command struct {
	Command string
	Inputs  []string
	Outputs []string
}

func (c *Command) ToKnit() []byte {
	buf := &bytes.Buffer{}
	buf.WriteString(fmt.Sprintf("$ %s: %s\n", strings.Join(c.Outputs, " "), strings.Join(c.Inputs, " ")))
	buf.WriteString(fmt.Sprintf("    %s", c.Command))
	return buf.Bytes()
}

func main() {
	runtime.LockOSThread()

	log.SetOutput(io.Discard)

	cmds, err := io.ReadAll(os.Stdin)
	if err != nil {
		log.Fatal(err)
	}

	lines := strings.Split(string(cmds), "\n")

	ex := &Excavation{}
	for _, line := range lines {
		if line == "" {
			continue
		}
		fmt.Fprintln(os.Stderr, line)
		in, out := excavate("sh", "-c", line)
		ex.Commands = append(ex.Commands, Command{
			Command: line,
			Inputs:  in,
			Outputs: out,
		})
	}
	fmt.Println(string(ex.ToKnit()))
}

// returns true if path is a subfile of dir
func subFile(dir, path string) bool {
	return path != "" && !strings.HasPrefix(path, "/")
}

func excavate(cmd string, args ...string) (in, out []string) {
	inputs := make(map[string]bool)
	outputs := make(map[string]bool)

	wd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	prog, _, err := NewProgram(cmd, args, Options{
		OnRead: func(path string) {
			if !subFile(wd, path) {
				return
			}
			if !outputs[path] {
				inputs[path] = true
			}
		},
		OnWrite: func(path string) {
			if !subFile(wd, path) {
				return
			}
			if inputs[path] {
				delete(inputs, path)
			}
			outputs[path] = true
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	var s Status
	for {
		p, err := prog.Wait(&s)
		if err == ErrFinishedTrace {
			break
		}
		if err != nil {
			log.Fatal(err)
		}

		if !p.Exited() {
			err = prog.Continue(p, s)
			if err != nil {
				fmt.Println(p.Pid(), err)
			}
		}
	}

	for f := range inputs {
		in = append(in, f)
	}
	for f := range outputs {
		out = append(out, f)
	}
	return in, out
}
