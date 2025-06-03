package storage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"ssoo-memoria/config"
	"ssoo-utils/codeutils"
	"strconv"
	"strings"
	"sync"
	"time"
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

func (p process_data) String() string {
	var msg string
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
	return msg
}

func GetDataByPID(pid uint) *process_data {
	for index, process := range systemMemory {
		if process.pid == pid {
			return &systemMemory[index]
		}
	}
	return nil
}

func GetIndexByPID(pid uint) int {
	for index, process := range systemMemory {
		if process.pid == pid {
			return index
		}
	}
	return -1
}

func GetInstruction(pid uint, pc int) (*instruction, error) {
	targetProcess := GetDataByPID(pid)
	if targetProcess == nil {
		return nil, errors.New("process pid=" + fmt.Sprint(pid) + " does not exist")
	}
	if pc >= len(targetProcess.code) {
		return nil, errors.New("out of scope program counter")
	}
	targetProcess.metrics.Instructions_requested++
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
		msg += p.String()
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
	index := GetIndexByPID(pidToDelete)
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
var userMemoryMutex sync.Mutex

func writeToMemory(index int, value byte) {
	userMemoryMutex.Lock()
	userMemory[index] = value
	userMemoryMutex.Unlock()
}

var nPages int
var pageBases []int
var reservationBits []bool

func GetRemainingMemory() int {
	return remainingMemory
}

func getFromPage(base int, delta int) (byte, error) {
	if base+delta > memorySize || delta >= paginationConfig.pageSize || delta < 0 {
		return 0, errors.New("out of bounds page memory access")
	}
	return userMemory[base+delta], nil
}

func StringToLogicAddress(str string) []int {
	slice := strings.Split(str, "|")
	addr := make([]int, len(slice))
	for i := range len(slice) {
		addr[i], _ = strconv.Atoi(slice[i])
	}
	return addr
}

func logicToPhysicalAddress(pid uint, address []int) (base int, delta int, processPageIndex int, err error) {
	levels := paginationConfig.levels
	pageTableSize := paginationConfig.entriesPerPage
	pageSize := paginationConfig.pageSize
	if len(address) != paginationConfig.levels+1 {
		err = errors.New(fmt.Sprint("address not matching pagination of ", levels, " levels"))
		return
	}
	delta = address[len(address)-1]
	address = address[:len(address)-1]

	var processPageBases []int
	process := GetDataByPID(pid)
	if process == nil {
		err = errors.New("could't find process with pid")
		return
	}
	processPageBases = process.pageBases

	var f_pageTableSize float64 = float64(pageTableSize)
	var f_levels float64 = float64(levels)
	for i, num := range address {
		if num < 0 || num >= pageTableSize {
			err = errors.New("out of bounds page table access")
			return
		}
		processPageIndex += num * int(math.Pow(f_pageTableSize, f_levels-1-float64(i)))
	}

	if processPageIndex >= len(processPageBases) {
		err = errors.New("out of bounds process memory access")
		return
	}

	base = processPageBases[processPageIndex]

	fmt.Println("Page:", processPageBases[processPageIndex]/pageSize, "Base: ", base, ". Delta: ", delta)

	return
}

func GetLogicAddress(pid uint, address []int) (byte, error) {
	base, delta, _, err := logicToPhysicalAddress(pid, address)
	if err != nil {
		return 0, err
	}
	return getFromPage(base, delta)
}

func WriteToLogicAddress(pid uint, address []int, value []byte) error {
	base, delta, index, err := logicToPhysicalAddress(pid, address)
	if err != nil {
		return err
	}
	processPageBases := GetDataByPID(pid).pageBases
	remainingSpace := 64*(len(processPageBases)-index) - delta
	if remainingSpace < len(value) {
		return errors.New("not enough space to write the value on this process assigned user memory at base")
	}

	for _, char := range value {
		writeToMemory(base+delta, char)
		delta++
		if delta >= 64 {
			delta = 0
			index++
			base = processPageBases[index]
		}
	}
	return nil
}

func pageToString(pageBase int) string {
	var msg string = "["
	for delta := range paginationConfig.pageSize {
		val, err := getFromPage(pageBase, delta)
		if err != nil {
			return "Error reading page. " + err.Error()
		}
		if val == 0 {
			msg += "˽"
		} else {
			msg += string(val)
		}
	}
	msg += "]\n"
	return msg
}

func Memory_Dump(pid uint) error {
	os.Mkdir(config.Values.DumpPath, 0755)
	processData := GetDataByPID(pid)
	if processData == nil {
		return errors.New("couldn't find process with pid")
	}
	dump_file, err := os.Create(config.Values.DumpPath + fmt.Sprint(processData.pid, "-", time.Now().Format("2006-01-02_15:04:05.9999")+".dmp"))
	if err != nil {
		return err
	}
	defer dump_file.Close()
	dump_file.WriteString("---------------( Process  Data )---------------\n")
	dump_file.WriteString(processData.String())
	dump_file.WriteString("---------------(     Pages     )---------------\n")
	dump_file.WriteString("| Index |  Base  | Content\n")
	for i, pageBase := range processData.pageBases {
		istr := fmt.Sprint(i)
		istr = strings.Repeat(" ", max(0, 5-len(istr))) + istr
		bstr := fmt.Sprint(pageBase)
		bstr = strings.Repeat(" ", max(0, 6-len(bstr))) + bstr
		dump_file.WriteString("| " + istr + " | " + bstr + " | " + pageToString(pageBase))
	}
	return nil
}

func InitializeUserMemory() {
	size := config.Values.MemorySize
	levels := config.Values.NumberOfLevels
	entriesPerPage := config.Values.EntriesPerPage
	pSize := config.Values.PageSize

	userMemory = make([]byte, size)
	remainingMemory = size
	memorySize = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)

	if levels <= 0 {
		return
	}

	paginationConfig = PaginationConfig{
		entriesPerPage: entriesPerPage,
		levels:         levels,
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
		return nil, errors.New("not enough memory")
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
	process_data := GetDataByPID(pid)
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
