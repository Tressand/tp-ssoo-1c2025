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

func CreateProcess(newpid uint, codeFile io.ReadCloser, memoryRequirement int) error {
	defer codeFile.Close()
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

	err := allocateMemory(newpid, memoryRequirement)
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

type config struct {
	size           int
	pageSize       int
	entriesPerPage int
	levels         int
}

var values config

var userMemory []byte
var remainingMemory = 0

func GetRemainingMemory() int {
	return remainingMemory
}

func logPage(page []interface{}, showValue bool) {
	var msg string = "----------\n"

	for index, entry := range page {
		var i string = fmt.Sprint(index)
		msg += i
		msg += strings.Repeat(" ", 6-len(i))
		msg += " | "
		if showValue {
			msg += fmt.Sprintf("%v", *entry.(*interface{}))
		} else {
			msg += fmt.Sprintf("%p", entry)
		}
		msg += "\n"
	}
	msg += "----------\n"
	fmt.Print(msg)
}

func paginate[T any](base []T, pageSize int) []interface{} {
	var pageList []interface{}
	totalPages := int(math.Ceil(float64(len(base)) / float64(pageSize)))

	for i_page := range totalPages {
		var newPage []interface{}
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

func multiLevelPaginate[T any](data []T, pageSize int, levels int) interface{} {
	if levels <= 0 {
		slog.Error("levels must be greater than 0")
		return nil
	}
	if pageSize <= 0 {
		slog.Error("pageSize must be greater than 0")
		return nil
	}

	var pages = paginate(data, pageSize)

	for range levels - 1 {
		nextLevel := paginate(pages, pageSize)
		pages = nextLevel
	}

	return pages
}

func getFromPage[BaseType any](page interface{}, index int) (interface{}, error) {
	elems, ok := page.([]interface{})
	if !ok {
		return nil, errors.New("slice conversion failed")
	}
	if index >= len(elems) {
		return nil, errors.New("index out of bounds")
	}
	elem := elems[index]
	ptr, ok := elem.(*interface{})
	if !ok {
		ptr, ok := elem.(*BaseType)
		if ok {
			return ptr, nil
		}
		return elem, nil
	}
	return *ptr, nil
}

func getFromPagination[BaseType any](pagination interface{}, indexes ...int) (interface{}, error) {
	if len(indexes) == 0 {
		return nil, errors.New("must indicate a valid sequence of indexes to search")
	}
	pages := pagination

	var elem interface{}
	var err error
	for i := range indexes {
		elem, err = getFromPage[BaseType](pages, indexes[i])
		if err != nil {
			return nil, err
		}
		if _, ok := elem.(*BaseType); ok {
			return elem, nil
		} else {
			pages = elem.([]interface{})
		}
	}

	return elem, nil
}

func InitializeUserMemory(size int, pageSize int, entriesPerPage int, levels int) {
	userMemory = make([]byte, size)
	remainingMemory = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)

	values = config{
		size:           size,
		pageSize:       pageSize,
		entriesPerPage: entriesPerPage,
		levels:         levels,
	}

	firstLevel := paginate(userMemory, pageSize)
	userMemory[0] = 'b'
	pagination := multiLevelPaginate(firstLevel, entriesPerPage, levels-1)
	page, err := getFromPagination[byte](pagination, 0, 0, 0, 0, 0)
	if err != nil {
		fmt.Println(err)
		return
	}
	list := page.([]interface{})
	msg := []byte("hola mundo")
	for i := range msg {
		cell := list[i].(*byte)
		*cell = msg[i]
	}
	fmt.Printf("\n%s\n\n", userMemory)
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
