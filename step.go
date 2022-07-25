package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"runtime"
	"strings"
)

type Step struct {
	Name     string
	Parents  []*Step
	Commands []Command
}

type Command struct {
	Name string
	Args []string

	inputs  []string
	outputs []string
}

func main() {
	runtime.LockOSThread()

	log.SetOutput(io.Discard)

	flag.Parse()
	args := flag.Args()

	target := args[0]
	args = args[1:]

	inputs := make(map[string]bool)
	outputs := make(map[string]bool)

	prog, _, err := NewProgram(target, args, Options{
		OnRead: func(path string) {
			if strings.HasSuffix(path, ".scala") {
				if !outputs[path] {
					inputs[path] = true
				}
			}
			// if len(path) == 0 || path[0] == '/' {
			// 	return
			// }
		},
		OnWrite: func(path string) {
			if len(path) == 0 || path[0] == '/' {
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
		fmt.Println("input:", f)
	}
	for f := range outputs {
		fmt.Println("output:", f)
	}
}
