package codeutils

type Opcode = int

type Instruction struct {
	Opcode Opcode
	Args   []string
}

const (
	NOOP Opcode = iota
	EXIT
	WRITE
	READ
	GOTO
	IO
	INIT_PROC
	DUMP_MEMORY
)

var OpcodeStrings map[int]string = map[int]string{
	NOOP:        "NOOP",
	EXIT:        "EXIT",
	WRITE:       "WRITE",
	READ:        "READ",
	GOTO:        "GOTO",
	IO:          "IO",
	INIT_PROC:   "INIT_PROC",
	DUMP_MEMORY: "DUMP_MEMORY",
}

func OpCodeFromString(str string) int {
	for key, value := range OpcodeStrings {
		if value == str {
			return key
		}
	}
	return -1
}
