package storage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"ssoo-utils/codeutils"
	"strings"
)

type instruction = codeutils.Instruction

var opcodeStrings map[codeutils.Opcode]string = codeutils.OpcodeStrings

//#region SECTION: SYSTEM MEMORY

type process_data struct {
	pid       uint
	code      []instruction
	pageBases []int
	metrics   memory_metrics
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

var systemMemory []process_data

type BasicProcessData struct {
	PID  uint
	Size int
}

func GetProcesses() []BasicProcessData {
	var processes []BasicProcessData
	for _, process := range systemMemory {
		processes = append(processes, BasicProcessData{
			PID:  process.pid,
			Size: len(process.pageBases) * paginationConfig.pageSize,
		})
	}
	return processes
}

func LogSystemMemory() {
	if len(systemMemory) == 0 {
		fmt.Println("No processes loaded.")
		return
	}
	var msg string
	for _, p := range systemMemory {
		msg += "------------------------\n"
		msg += "|  PID: " + fmt.Sprint(p.pid) + "\n|\n"
		msg += "|  Reserved pages: ["
		for _, base := range p.pageBases {
			msg += fmt.Sprint(base/paginationConfig.pageSize) + ", "
		}
		msg = msg[:len(msg)-2] + "]\n|\n"
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

func CreateProcess(newpid uint, codeFile io.Reader, memoryRequirement int) error {
	if memoryRequirement > remainingMemory {
		return errors.New("not enough user memory")
	}

	newProcessData := new(process_data)
	newProcessData.pid = newpid

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

	reservedPageBases, err := allocateMemory(memoryRequirement)
	if err != nil {
		slog.Error("failed memory allocation", "error", err)
		return err
	}

	newProcessData.pageBases = reservedPageBases

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

type PaginationConfig struct {
	pageSize       int
	entriesPerPage int
	levels         int
}

var paginationConfig PaginationConfig

var memorySize int
var remainingMemory = 0
var userMemory []byte

var nPages int
var pageBases []int
var reservationBits []bool

func GetRemainingMemory() int {
	return remainingMemory
}

func logPage(pageBase int) {
	var msg string = "----------\n"

	for delta := range paginationConfig.pageSize {
		var i string = fmt.Sprint(delta)
		msg += fmt.Sprint(delta)
		msg += strings.Repeat(" ", 6-len(i))
		msg += " | "
		msg += fmt.Sprint(userMemory[pageBase+delta])
		msg += "\n"
	}
	msg += "----------\n"
	fmt.Print(msg)
}

func InitializeUserMemory(size int, pSize int, entriesPerPage int, levels int) {
	userMemory = make([]byte, size)
	remainingMemory = size
	memorySize = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)

	if levels <= 0 {
		return
	}

	paginationConfig = PaginationConfig{
		entriesPerPage: entriesPerPage,
		levels:         levels - 1,
		pageSize:       pSize,
	}
	nPages = memorySize / pSize
	if memorySize%pSize != 0 {
		slog.Error("memory size couldn't be equally subdivided,"+
			"remainder memory unaccessible to prevent errors",
			"memorySize", memorySize, "pageSize", pSize)
	}
	pageBases = make([]int, nPages)
	for i := range nPages {
		pageBases[i] = i * pSize
	}
	reservationBits = make([]bool, nPages)
	slog.Info("Paginación realizada", "cantidad_de_paginas", nPages)
}

func allocateMemory(size int) ([]int, error) {
	if size > remainingMemory {
		return nil, errors.New("not enough memory.")
	}
	requiredPages := int(math.Ceil(float64(size) / float64(paginationConfig.pageSize)))
	slog.Info("allocating memory", "bytes", size, "pages", requiredPages)
	remainingMemory -= paginationConfig.pageSize * requiredPages
	if remainingMemory < 0 {
		panic("memory got negative, wtf.")
	}
	var processPageBases []int = make([]int, requiredPages)

	i := 0
	for index, pageBase := range pageBases {
		if !reservationBits[index] {
			reservationBits[index] = true
			processPageBases[i] = pageBase
			i++
		}
		if i == requiredPages {
			return processPageBases, nil
		}
	}
	return nil, errors.New("something wrong ocurred on memory allocation")
}

func deallocateMemory(pid uint) error {
	process_data, _ := GetDataByPID(pid)
	if process_data == nil {
		return errors.New("couldn't find process with id")
	}
	for _, pageBase := range process_data.pageBases {
		reservationBits[pageBase/paginationConfig.pageSize] = false
	}
	remainingMemory += len(process_data.pageBases) * paginationConfig.pageSize
	return nil
}

//#endregion
