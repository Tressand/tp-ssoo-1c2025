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
	pid           uint
	code          []instruction
	pageTable     []any
	reservedPages []int
	metrics       memory_metrics
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
		processes = append(processes, BasicProcessData{PID: process.pid, Size: len(process.reservedPages) * pageSize})
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
		msg += "|  Reserved pages: " + fmt.Sprint(p.reservedPages) + "\n|\n"
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

	pageTable, reservedPages, err := allocateMemory(memoryRequirement)
	if err != nil {
		return err
	}

	newProcessData.pageTable = pageTable
	newProcessData.reservedPages = reservedPages

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

type PageTableConfig struct {
	entriesPerPage int
	levels         int
}

var pageTableConfig PageTableConfig

var memorySize int
var remainingMemory = 0
var userMemory []byte

var pageSize int
var pagination [][]byte

var reservationBits []bool

func GetRemainingMemory() int {
	return remainingMemory
}

func logPage(page []any, showValue bool) {
	var msg string = "----------\n"

	for index, entry := range page {
		var i string = fmt.Sprint(index)
		msg += i
		msg += strings.Repeat(" ", 6-len(i))
		msg += " | "
		if showValue {
			msg += fmt.Sprintf("%v", *entry.(*any))
		} else {
			msg += fmt.Sprintf("%p", entry)
		}
		msg += "\n"
	}
	msg += "----------\n"
	fmt.Print(msg)
}

func untypedPaginate[T any](base []T, pageSize int) []any {
	var pageList []any
	totalPages := int(math.Ceil(float64(len(base)) / float64(pageSize)))

	for i_page := range totalPages {
		var newPage []any
		for i_value := range pageSize {
			index := i_page*pageSize + i_value
			if index > (len(base) - 1) {
				newPage = append(newPage, nil)
				continue
			}
			newPage = append(newPage, &base[i_page*pageSize+i_value])
		}
		pageList = append(pageList, newPage)
	}

	return pageList
}

func typedPaginate[T any](base []T, pageSize int) [][]T {
	if pageSize == 0 {
		return nil
	}
	var pageList [][]T
	nPages := int(math.Ceil(float64(len(base)) / float64(pageSize)))
	for i := range nPages {
		start := i * pageSize
		end := min(start+pageSize, len(base))
		pageList = append(pageList, base[start:end])
	}
	return pageList
}

func typedPointerPaginate[T any](base []T, pageSize int) [][]*T {
	if pageSize == 0 {
		return nil
	}
	var pageList [][]*T
	nPages := int(math.Ceil(float64(len(base)) / float64(pageSize)))
	for i := range nPages {
		var page []*T = make([]*T, pageSize)
		start := i * pageSize
		for delta := range pageSize {
			if start+delta > len(base) {
				page[delta] = nil
			}
			page[delta] = &base[start+delta]
		}
		pageList = append(pageList, page)
	}
	return pageList
}

func multiLevelPaginate[T any](data []T, pageSize int, levels int) any {
	if levels <= 0 || pageSize <= 0 {
		return data
	}

	var pages = untypedPaginate(data, pageSize)

	for range levels - 1 {
		nextLevel := untypedPaginate(pages, pageSize)
		pages = nextLevel
	}

	return pages
}

func getFromPage[BaseType any](page any, index int) (any, error) {
	elems, ok := page.([]any)
	if !ok {
		elems, ok := page.([]BaseType)
		if ok {
			return &elems[index], nil
		}
		return nil, errors.New("slice conversion failed")
	}
	if index >= len(elems) {
		return nil, errors.New("index out of bounds")
	}
	elem := elems[index]
	ptr, ok := elem.(*any)
	if !ok {
		ptr, ok := elem.(*BaseType)
		if ok {
			return ptr, nil
		}
		return elem, nil
	}
	return *ptr, nil
}

func getFromPagination[BaseType any](pagination any, indexes ...int) (any, error) {
	if len(indexes) == 0 {
		return nil, errors.New("must indicate a valid sequence of indexes to search")
	}
	pages := pagination

	var elem any
	var err error
	for i := range indexes {
		elem, err = getFromPage[BaseType](pages, indexes[i])
		if err != nil {
			return nil, err
		}
		if _, ok := elem.(*BaseType); ok {
			return elem, nil
		} else {
			pages, ok = elem.([]any)
			if !ok {
				if _, ok = elem.(*[]BaseType); !ok {
					return pages, errors.New("couldn't convert elem to page")
				}
				elem = *elem.(*[]BaseType)
				pages = elem
			}
		}
	}

	return elem, nil
}

func InitializeUserMemory(size int, pSize int, entriesPerPage int, levels int) {
	userMemory = make([]byte, size)
	remainingMemory = size
	memorySize = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)

	if levels <= 0 {
		return
	}

	pageSize = pSize
	pagination = typedPaginate(userMemory, pageSize)
	reservationBits = make([]bool, len(pagination))
	slog.Info("Paginación realizada", "cantidad_de_paginas", len(pagination))

	pageTableConfig = PageTableConfig{
		entriesPerPage: entriesPerPage,
		levels:         levels - 1,
	}
}

func allocateMemory(size int) ([]any, []int, error) {
	requiredPages := int(math.Ceil(float64(size) / float64(pageSize)))
	slog.Info("allocating memory", "bytes", size, "pages", requiredPages)
	remainingMemory -= pageSize * requiredPages
	if remainingMemory < 0 {
		panic("memory got negative, wtf.")
	}
	var processPages []any = make([]any, len(pagination))
	var reservedPages []int

	i := 0
	for index, page := range pagination {
		if !reservationBits[index] {
			reservationBits[index] = true
			reservedPages = append(reservedPages, index)
			processPages[i] = &page
			i++
		}
		if i == requiredPages {
			break
		}
	}
	processPagination := multiLevelPaginate(processPages, pageTableConfig.entriesPerPage, pageTableConfig.levels).([]any)
	return processPagination, reservedPages, nil
}

func deallocateMemory(pid uint) error {
	process_data, _ := GetDataByPID(pid)
	if process_data == nil {
		return errors.New("couldn't find process with id")
	}
	for _, pageIndex := range process_data.reservedPages {
		reservationBits[pageIndex] = false
	}
	remainingMemory += len(process_data.reservedPages) * pageSize
	return nil
}

//#endregion
