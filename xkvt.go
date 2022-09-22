package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/pflag"
)

func fatal(msg ...interface{}) {
	fmt.Fprintln(os.Stderr, msg...)
	os.Exit(1)
}

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

func (e *Excavation) ToMake() []byte {
	buf := &bytes.Buffer{}
	for _, c := range e.Commands {
		buf.Write(c.ToMake())
		buf.WriteByte('\n')
	}
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

func (c *Command) ToMake() []byte {
	buf := &bytes.Buffer{}
	outputs := strings.Join(c.Outputs, " ")
	if len(c.Outputs) > 1 {
		outputs += " &"
	}
	buf.WriteString(fmt.Sprintf("%s: %s\n", outputs, strings.Join(c.Inputs, " ")))
	buf.WriteString(fmt.Sprintf("\t%s", c.Command))
	return buf.Bytes()
}

func main() {
	runtime.LockOSThread()

	verbose := pflag.BoolP("verbose", "V", false, "verbose debugging information")
	format := pflag.StringP("format", "f", "json", "output format")
	output := pflag.StringP("output", "o", "", "output file")
	input := pflag.StringP("input", "i", "", "input file")
	help := pflag.BoolP("help", "h", false, "show this help message")

	pflag.Parse()

	if *help {
		pflag.Usage()
		os.Exit(0)
	}

	if !*verbose {
		log.SetOutput(io.Discard)
	}

	var inf io.Reader
	if *input != "" {
		f, err := os.Open(*input)
		if err != nil {
			fatal(err)
		}
		inf = f
		defer f.Close()
	} else {
		inf = os.Stdin
	}

	cmds, err := io.ReadAll(inf)
	if err != nil {
		fatal(err)
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
	var out []byte
	switch *format {
	case "json":
		data, err := ex.ToJson()
		if err != nil {
			fatal(err)
		}
		out = data
	case "knit":
		out = ex.ToKnit()
	case "make":
		out = ex.ToMake()
	default:
		fatal(fmt.Sprintf("unknown format '%s'", *format))
	}

	var outf io.Writer
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fatal(err)
		}
		outf = f
		defer f.Close()
	} else {
		outf = os.Stdout
	}

	outf.Write(out)
	outf.Write([]byte{'\n'})
}

// returns true if path is a subfile of dir
func subFile(dir, path string) bool {
	return strings.HasPrefix(path, dir)
}

func excavate(cmd string, args ...string) (in, out []string) {
	inputs := make(map[string]bool)
	outputs := make(map[string]bool)

	wd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	prog, _, err := NewProgram(cmd, args, Options{
		OnRead: func(path string) {
			log.Println("rd", path)
			if !subFile(wd, path) {
				return
			}
			if !outputs[path] {
				inputs[path] = true
			}
		},
		OnWrite: func(path string) {
			log.Println("wr", path)
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
		fatal(err)
	}
	var s Status
	for {
		p, err := prog.Wait(&s)
		if err == ErrFinishedTrace {
			break
		}
		if err != nil {
			fatal(err)
		}

		if !p.Exited() {
			err = prog.Continue(p, s)
			if err != nil {
				fmt.Println(p.Pid(), err)
			}
		}
	}

	for f := range inputs {
		p, err := filepath.Rel(wd, f)
		if err != nil {
			fatal(err)
		}
		in = append(in, p)
	}
	for f := range outputs {
		p, err := filepath.Rel(wd, f)
		if err != nil {
			fatal(err)
		}
		out = append(out, p)
	}
	return in, out
}
