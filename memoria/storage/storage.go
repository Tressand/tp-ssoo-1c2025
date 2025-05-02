package storage

import (
	"bufio"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"ssoo-utils/codeutils"
	"strings"
)

type instruction = codeutils.Instruction

var opcodeStrings map[int]string = codeutils.OpcodeStrings

//#region SECTION: SYSTEM MEMORY

type process_data struct {
	pid     uint
	code    []instruction
	metrics memory_metrics
}

func GetDataByPID(pid uint) (data *process_data, index int) {
	for index, process := range systemMemory {
		if process.pid == pid {
			return &systemMemory[index], index
		}
	}
	return nil, -1
}

func GetInstruction(pid uint, pc int) (*instruction, error) {
	targetProcess, _ := GetDataByPID(pid)
	if targetProcess == nil {
		return nil, errors.New("process pid=" + fmt.Sprint(pid) + " does not exist")
	}
	if pc >= len(targetProcess.code) {
		return nil, errors.New("out of scope program counter")
	}
	return &targetProcess.code[pc], nil
}

var systemMemory []process_data = make([]process_data, 0)

func LogSystemMemory() {
	if len(systemMemory) == 0 {
		fmt.Println("No processes loaded.")
		return
	}
	var msg string
	for _, p := range systemMemory {
		msg += "------------------------\n"
		msg += "|  PID: " + fmt.Sprint(p.pid) + "\n|\n"
		msg += "|  Code (" + fmt.Sprint(len(p.code)) + " instructions)\n"
		for index, inst := range p.code {
			msg += "|    " + opcodeStrings[inst.Opcode] + " " + fmt.Sprint(inst.Args) + "\n"
			if index >= 10 {
				msg += "|    (...)\n"
				break
			}
		}
	}
	msg += "------------------------\n"
	fmt.Print(msg)
}

func CreateProcess(newpid uint, codePath string, memoryRequirement int) error {
	if memoryRequirement > remainingMemory {
		return errors.New("not enough user memory")
	}

	newProcessData := new(process_data)
	newProcessData.pid = newpid

	codeFile, err := os.OpenFile(codePath, os.O_RDONLY, 0666)
	if err != nil {
		return err
	}
	defer codeFile.Close()

	scanner := bufio.NewScanner(codeFile)
	for scanner.Scan() {
		if scanner.Err() != nil {
			return scanner.Err()
		}
		line := scanner.Text()
		parts := strings.Split(line, " ")
		if len(parts) > 3 {
			return errors.New("more arguments than possible")
		}
		newOpCode := codeutils.OpCodeFromString(parts[0])
		if newOpCode == -1 {
			return errors.New("opcode not recognized")
		}
		newProcessData.code = append(newProcessData.code, instruction{Opcode: newOpCode, Args: parts[1:]})
	}

	err = allocateMemory(newpid, memoryRequirement)
	if err != nil {
		return err
	}

	systemMemory = append(systemMemory, *newProcessData)
	return nil
}

func DeleteProcess(pidToDelete uint) error {
	_, index := GetDataByPID(pidToDelete)
	if index == -1 {
		return errors.New("could not find pid to delete")
	}
	err := deallocateMemory(pidToDelete)
	if err != nil {
		return err
	}

	systemMemory[index] = systemMemory[len(systemMemory)-1]
	systemMemory = systemMemory[:len(systemMemory)-1]
	return nil
}

//#endregion

//#region SECTION: USER MEMORY

type memory_metrics struct {
	Page_table_accesses    int
	Instructions_requested int
	Suspensions            int
	Unsuspensions          int
	Reads                  int
	Writes                 int
}

var userMemory []byte
var remainingMemory = 0

func GetRemainingMemory() int {
	return remainingMemory
}

func InitializeUserMemory(size int) {
	userMemory = make([]byte, size)
	remainingMemory = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)
}

func allocateMemory(pid uint, size int) error {
	// Not implemented
	return nil
}

func deallocateMemory(pid uint) error {
	// Not implemented
	return nil
}

//#endregion
