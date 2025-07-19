package storage

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"slices"
	"ssoo-memoria/config"
	"ssoo-utils/codeutils"
	"ssoo-utils/logger"
	"strconv"
	"strings"
	"sync"
	"time"
)

//#region SECTION: SYSTEM MEMORY

type instruction = codeutils.Instruction

var opcodeStrings map[codeutils.Opcode]string = codeutils.OpcodeStrings

type process_data struct {
	pid       uint
	code      []instruction
	pageBases []int
	metrics   memory_metrics
}

var systemMemoryMutex sync.Mutex
var systemMemory []process_data

func (p process_data) String() string {
	var msg string
	msg += "|  PID: " + fmt.Sprint(p.pid) + "\n|\n"
	msg += "|  Reserved pages: ["
	for _, base := range p.pageBases {
		msg += fmt.Sprint(base/paginationConfig.PageSize) + ", "
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

func (p process_data) Deallocate() error {
	err := deallocateMemory(p.pid)
	if err != nil {
		return err
	}
	for i := range systemMemory {
		if systemMemory[i].pid == p.pid {
			systemMemoryMutex.Lock()
			systemMemory[i] = systemMemory[len(systemMemory)-1]
			systemMemory = systemMemory[:len(systemMemory)-1]
			systemMemoryMutex.Unlock()
			break
		}
	}
	m := p.metrics
	logger.RequiredLog(true, p.pid, "Proceso Destruido - Métricas", map[string]string{
		"Acc.T.Pag": fmt.Sprint(m.Page_table_accesses),
		"Inst.Sol.": fmt.Sprint(m.Instructions_requested),
		"SWAP":      fmt.Sprint(m.Suspensions),
		"Mem.Prin.": fmt.Sprint(m.Unsuspensions),
		"Lec.Mem.":  fmt.Sprint(m.Reads),
		"Esc.Mem.":  fmt.Sprint(m.Writes),
	})
	return nil
}

func GetDataByPID(pid uint) *process_data {
	for index, process := range systemMemory {
		if process.pid == pid {
			return &systemMemory[index]
		}
	}
	return nil
}

func GetInstruction(pid uint, pc int) (instruction, error) {
	targetProcess := GetDataByPID(pid)
	if targetProcess == nil {
		return instruction{Opcode: codeutils.EXIT}, errors.New("process pid=" + fmt.Sprint(pid) + " does not exist")
	}
	if pc >= len(targetProcess.code) {
		return instruction{Opcode: codeutils.EXIT}, errors.New("out of scope program counter")
	}
	targetProcess.metrics.Instructions_requested++

	inst := targetProcess.code[pc]
	logger.RequiredLog(true, pid, "Obtener Instrucción: "+fmt.Sprint(pc), map[string]string{
		"Instrucción": fmt.Sprintf("(%s %v)", opcodeStrings[inst.Opcode], inst.Args),
	})

	return inst, nil
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

	if memoryRequirement != 0 {
		reservedPageBases, err := allocateMemory(memoryRequirement)
		if err != nil {
			slog.Error("failed memory allocation", "error", err)
			return err
		}

		newProcessData.pageBases = reservedPageBases
	}
	systemMemoryMutex.Lock()
	systemMemory = append(systemMemory, *newProcessData)
	systemMemoryMutex.Unlock()

	logger.RequiredLog(true, newpid, "Proceso Creado", map[string]string{"Tamaño": fmt.Sprint(memoryRequirement)})
	return nil
}

func DeleteProcess(pidToDelete uint) error {
	process_data := GetDataByPID(pidToDelete)
	if process_data == nil {
		return errors.New("could not find pid to delete")
	}
	err := process_data.Deallocate()
	if err != nil {
		return err
	}
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
	PageSize       int `json:"page_size"`
	EntriesPerPage int `json:"entries_per_page"`
	Levels         int `json:"levels"`
}

var paginationConfig PaginationConfig

func GetConfig() PaginationConfig { return paginationConfig }

//#region DATA ARRAY

var memorySize int
var remainingMemory = 0
var userMemory []byte
var userMemoryMutex sync.Mutex

func GetFromMemory(pid uint, base int, delta int) (byte, error) {
	if ok, err := HasPage(pid, base); !ok {
		return 0, err
	}
	if base+delta > memorySize || delta >= paginationConfig.PageSize || delta < 0 {
		return 0, errors.New("out of bounds page memory access")
	}
	GetDataByPID(pid).metrics.Reads++
	return userMemory[base+delta], nil
}

func WriteToMemory(pid uint, base int, delta int, value byte) error {
	if ok, err := HasPage(pid, base); !ok {
		return err
	}
	userMemoryMutex.Lock()
	userMemory[base+delta] = value
	userMemoryMutex.Unlock()

	GetDataByPID(pid).metrics.Writes++

	return nil
}

func GetRemainingMemory() int {
	return remainingMemory
}

//#endregion

//#region PAGES

var nPages int
var pageBases []int
var reservationBits []bool

func HasPage(pid uint, base int) (bool, error) {
	processData := GetDataByPID(pid)
	if processData == nil {
		return false, errors.New("couldn't find process with pid")
	}
	if !slices.Contains(processData.pageBases, base) {
		return false, errors.New("process does not have this page assigned")
	}
	return true, nil
}

func GetPage(pid uint, base int) ([]byte, error) {
	if ok, err := HasPage(pid, base); !ok {
		return nil, err
	}
	GetDataByPID(pid).metrics.Reads += config.Values.PageSize
	return userMemory[base : base+paginationConfig.PageSize], nil
}

func WritePage(pid uint, base int, value []byte) error {
	if len(value) != paginationConfig.PageSize {
		return errors.New("write body does not match page size")
	}
	if ok, err := HasPage(pid, base); !ok {
		return err
	}
	for delta, char := range value {
		if err := WriteToMemory(pid, base, delta, char); err != nil {
			return err
		}
	}
	return nil
}

//#endregion

//#region ADDRESSES

func StringToLogicAddress(str string) []int {
	slice := strings.Split(str, "|")
	addr := make([]int, len(slice))
	for i := range len(slice) {
		addr[i], _ = strconv.Atoi(slice[i])
	}
	return addr
}

func LogicAddressToFrame(pid uint, address []int) (base int, err error) {
	levels := paginationConfig.Levels
	pageTableSize := paginationConfig.EntriesPerPage
	if len(address) != paginationConfig.Levels {
		err = errors.New(fmt.Sprint("address not matching pagination of ", levels, " levels"))
		return
	}

	process := GetDataByPID(pid)
	if process == nil {
		err = errors.New("could't find process with pid")
		return
	}
	processPageBases := process.pageBases
	processPageIndex := 0

	var f_pageTableSize float64 = float64(pageTableSize)
	var f_levels float64 = float64(levels)
	for i, num := range address {
		if num < 0 || num >= pageTableSize {
			err = errors.New("out of bounds page table access")
			return
		}
		processPageIndex += num * int(math.Pow(f_pageTableSize, f_levels-1-float64(i)))
		time.Sleep(time.Duration(config.Values.MemoryDelay) * time.Millisecond)
		process.metrics.Page_table_accesses++
	}

	if processPageIndex >= len(processPageBases) {
		err = errors.New("out of bounds process memory access")
		return
	}

	base = processPageBases[processPageIndex]
	return
}

//#endregion

//#region MEMORY DUMP

func pageToByteArray(pageBase int) ([]byte, error) {
	if !slices.Contains(pageBases, pageBase) {
		return nil, errors.New("page base not valid")
	}
	return userMemory[pageBase : pageBase+paginationConfig.PageSize], nil
}

func pageToString(pageBase int) string {
	var msg string = "["
	page, err := pageToByteArray(pageBase)
	if err != nil {
		return "Error reading page. " + err.Error()
	}
	for _, val := range page {
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

	logger.RequiredLog(true, pid, "Memory Dump Solicitado", map[string]string{})
	return nil
}

//#endregion

//#region SECTION: SWAP

var swapMutex sync.Mutex

func formatSwapData(data *process_data) (msg string) {
	msg += fmt.Sprint(data.pid, "|", len(data.pageBases), "\n")

	for _, base := range data.pageBases {
		bytes, _ := GetPage(data.pid, base)
		if bytes == nil {
			return ""
		}
		msg += fmt.Sprint(bytes, "\n")
	}

	msg += "~\n"
	return
}

func addToSwap(data *process_data) error {
	swapFile, err := os.OpenFile(config.Values.SwapfilePath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		return err
	}
	defer swapFile.Close()

	_, err = swapFile.WriteString(formatSwapData(data))
	return err
}

func numberFromReader(r *bufio.Reader, delim byte) (int, error) {
	readValue, err := r.ReadString(delim)
	if err != nil {
		return 0, err
	}
	result, err := strconv.Atoi(readValue[:len(readValue)-1])
	return result, err
}

func getFromSwap(data *process_data) (string, error) {
	var swapFile *os.File
	var pid uint = data.pid

	swapFile, err := os.OpenFile(config.Values.SwapfilePath, os.O_RDONLY, 0666)
	if err != nil {
		slog.Error(err.Error())
		return "", err
	}
	defer swapFile.Close()

	reader := bufio.NewReader(swapFile)

	for {
		// Get pid from swap block
		h_pid, err := numberFromReader(reader, '|')
		if err != nil {
			if err != io.EOF {
				slog.Error(err.Error())
			}
			return "", err
		}

		data, err := reader.ReadString('~')
		if err != nil {
			if err != io.EOF {
				slog.Error(err.Error())
			}
			return "", err
		}

		// Check if block pid is target pid
		if uint(h_pid) == pid {
			return data, err
		}
		_, err = reader.Discard(1)
		if err != nil {
			if err != io.EOF {
				slog.Error(err.Error())
			}
			return "", err
		}
	}
}

func removeFromSwap(pid uint) error {
	swapFile, err := os.OpenFile(config.Values.SwapfilePath, os.O_RDONLY, 0666)
	if err != nil {
		return err
	}
	defer swapFile.Close()

	temp_dir_path := strings.TrimSuffix(config.Values.SwapfilePath, "swapfile.bin")
	temp_swapFile, err := os.CreateTemp(temp_dir_path, "swapfile_*.tmp")
	if err != nil {
		return err
	}
	defer func() {
		os.Remove(temp_swapFile.Name())
		temp_swapFile.Close()
	}()

	reader := bufio.NewReader(swapFile)

	var finished bool = false
	for !finished {
		h_s_pid, err := reader.ReadString('|')
		if err != nil {
			return err
		}
		h_pid, err := strconv.Atoi(h_s_pid[:len(h_s_pid)-1])
		if err != nil {
			return err
		}

		s_block, err := reader.ReadString('~')
		if err != nil {
			return err
		}
		reader.Discard(1)

		if uint(h_pid) == pid {
			for {
				line, err := reader.ReadString('\n')
				temp_swapFile.WriteString(line)
				if err == io.EOF {
					finished = true
					break
				}
			}

		} else {
			temp_swapFile.WriteString(h_s_pid)
			temp_swapFile.WriteString(s_block + "\n")
		}
	}

	os.Rename(temp_swapFile.Name(), swapFile.Name())
	return nil
}

func SuspendProcess(pid uint) error {
	log_msg := fmt.Sprintf("Kernel solicita bajar PID %v a SWAP. ", pid)
	process_data := GetDataByPID(pid)
	if process_data == nil {
		return errors.New("could not find process with pid")
	}
	if len(process_data.pageBases) == 0 {
		slog.Info(log_msg + "Nada que swapear.")
		return nil
	}
	slog.Info(log_msg)

	swapMutex.Lock()
	err := addToSwap(process_data)
	swapMutex.Unlock()
	if err != nil {
		return err
	}

	err = deallocateMemory(pid)
	if err != nil {
		return err
	}

	process_data.metrics.Suspensions++
	return err
}

func UnSuspendProcess(pid uint) error {
	log_msg := fmt.Sprintf("Kernel solicita subir PID %v de SWAP. ", pid)

	process_data := GetDataByPID(pid)
	if process_data == nil {
		return errors.New("could not find process with pid")
	}
	if len(process_data.pageBases) == 0 {
		slog.Info(log_msg + "Nada que subir.")
		return nil
	}
	slog.Info(log_msg)

	swapMutex.Lock()
	swapBlock, err := getFromSwap(process_data)
	swapMutex.Unlock()

	if err != nil {
		if err == io.EOF {
			return errors.New("could not find process in swapfile. is it even suspended?")
		}
		return err
	}
	chunks := strings.Split(swapBlock, "\n")
	page_count, _ := strconv.Atoi(chunks[0])
	pageBases, err := allocateMemory(config.Values.PageSize * page_count)
	if err != nil {
		return err
	}

	swapMutex.Lock()
	err = removeFromSwap(pid)
	swapMutex.Unlock()

	if err != nil {
		return err
	}

	process_data.pageBases = pageBases

	//drop pagecount and block separator from chunks
	chunks = chunks[1 : len(chunks)-1]

	var bytes []byte = make([]byte, config.Values.PageSize)
	for i_chunk, base := range pageBases {
		var page_str string = chunks[i_chunk]
		s_page := strings.Split(page_str[1:len(page_str)-1], " ")
		for i_byte, char := range s_page {
			char_int, _ := strconv.Atoi(char)
			bytes[i_byte] = byte(char_int)
		}
		WritePage(pid, base, bytes)
	}

	process_data.metrics.Unsuspensions++

	return nil
}

//#endregion

// #region INITIALIZE
func InitializeUserMemory() {
	size := config.Values.MemorySize
	levels := config.Values.NumberOfLevels
	entriesPerPage := config.Values.EntriesPerPage
	pSize := config.Values.PageSize

	userMemory = make([]byte, size)
	remainingMemory = size
	memorySize = size
	slog.Info("Memoria de Usuario Inicializada", "size", remainingMemory)

	os.Remove(config.Values.SwapfilePath)

	if levels <= 0 {
		return
	}

	paginationConfig = PaginationConfig{
		EntriesPerPage: entriesPerPage,
		Levels:         levels,
		PageSize:       pSize,
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

//#endregion

//#region MALLOC/FREE

func allocateMemory(size int) ([]int, error) {
	if size > remainingMemory {
		return nil, errors.New("not enough memory")
	}
	requiredPages := int(math.Ceil(float64(size) / float64(paginationConfig.PageSize)))
	slog.Info("allocating memory", "bytes", size, "pages", requiredPages)
	remainingMemory -= paginationConfig.PageSize * requiredPages
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
	slog.Info("deallocating memory", "pid", pid, "size", len(process_data.pageBases)*config.Values.PageSize)
	if len(process_data.pageBases) == 0 {
		return nil
	}
	for _, pageBase := range process_data.pageBases {
		reservationBits[pageBase/paginationConfig.PageSize] = false
	}
	remainingMemory += len(process_data.pageBases) * paginationConfig.PageSize
	return nil
}

//#endregion

//#endregion
