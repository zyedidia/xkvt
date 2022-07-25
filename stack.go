package main

type FuncStack struct {
	entries []ExitFunc
}

func NewStack() *FuncStack {
	return &FuncStack{
		entries: make([]ExitFunc, 0, 4),
	}
}

func (s *FuncStack) Size() int {
	return len(s.entries)
}

func (s *FuncStack) Reset() {
	s.entries = s.entries[:1]
}

func (s *FuncStack) Push(ent ExitFunc) {
	s.entries = append(s.entries, ent)
}

func (s *FuncStack) Pop() ExitFunc {
	if len(s.entries) == 0 {
		return nil
	}

	ret := s.entries[len(s.entries)-1]
	s.entries = s.entries[:len(s.entries)-1]
	return ret
}

func (s *FuncStack) Peek() ExitFunc {
	if len(s.entries) == 0 {
		return nil
	}
	return s.entries[len(s.entries)-1]
}
