package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	cache "ssoo-cpu/memory"
	"ssoo-utils/codeutils"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"strconv"
	"time"
)

type Instruction = codeutils.Instruction

var instruction Instruction

var shutdownSignal = make(chan any)

var bloqueante = false

var status = 0

var fetch = false

func main() {
	//Obtener identificador
	if len(os.Args) < 2 {
		fmt.Println("Falta el identificador. Uso: ./cpu [identificador]")
		return
	}
	identificadorStr := os.Args[1]
	identificador, err1 := strconv.Atoi(identificadorStr)
	if err1 != nil {
		fmt.Printf("Error al convertir el identificador '%s' a entero: %v\n", identificadorStr, err1)
		return
	}
	config.Identificador = identificador
	fmt.Printf("Identificador recibido: %d\n", config.Identificador) //funciona falta saberlo usar

	//cargar config
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	config.Values.PortCPU += identificador

	if config.Values.CacheEntries != 0 {
		config.CacheEnable = true
		cache.InitCache()
	} else {
		config.CacheEnable = false
	}

	kernelPing := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "/ping",
	})
	_, err := http.Get(kernelPing)
	if err != nil {
		fmt.Println("Esperando a Kernel")
	}
	for err != nil {
		time.Sleep(1 * time.Second)
		_, err = http.Get(kernelPing)
	}
	memoryPing := httputils.BuildUrl(httputils.URLData{
        Ip:       config.Values.IpMemory,
        Port:     config.Values.PortMemory,
        Endpoint: "/ping",
    })
    _, err = http.Get(memoryPing)
    if err != nil {
        fmt.Println("Esperando a Memoria")
    }
    for err != nil {
        time.Sleep(1 * time.Second)
        _, err = http.Get(memoryPing)
    }


	//cargar config de memoria
	cache.FindMemoryConfig()

	//crear logger
	err = logger.SetupDefault("cpu"+ identificadorStr, config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	slog.Info("Arranca CPU")

	//iniciar tlb
	cache.InitTLB(config.Values.TLBEntries, config.Values.TLBReplacement)

	//iniciar server

	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/interrupt", interrupt())
	mux.Handle("/dispatch", receivePIDPC())
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		go func() {
			fmt.Println("Se solició cierre. o7")
			shutdownSignal <- struct{}{}
			<-shutdownSignal
			os.Exit(0)
		}()
	})

	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	notifyKernel(identificadorStr)
	select {}
}

func ciclo() {
	fetch = false
	config.Instruccion = ""

	for {
		fmt.Println()
		logger.RequiredLog(true, uint(config.Pcb.PID), "FETCH", map[string]string{
			"Program Counter": fmt.Sprint(config.Pcb.PC),
		})

		//fetch
		ok := sendPidPcToMemory(config.Pcb.PC,config.Pcb.PID)
		if !ok {
			slog.Warn("No se pudo obtener la instrucción, se reintentará en 100ms")
			time.Sleep(100 * time.Millisecond)
			continue 
		}

		//decode
		asign()

		//execute
		status := exec()

		select {
		case <-config.InterruptChan:
			sendResults(config.Pcb.PID, config.Pcb.PC, "Interrupt")
			config.FinishBeforeInterrupt<- struct{}{}
			return
		case <-config.ExitChan:
			sendResults(config.Pcb.PID, config.Pcb.PC, "Exit")
			return
		default:
		}

		if status == -1 {
			return
		}

		time.Sleep(100 * time.Millisecond)
	}
}

//#region FETCH

func sendPidPcToMemory(PC int, PID int) bool{

	logger.RequiredLog(false,uint(PID),"Searching Instruction",map[string]string{
		"PID": fmt.Sprint(PID),
		"PC": fmt.Sprint(PC),
	})

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(PID),
			"pc":  fmt.Sprint(PC),
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ","PC",fmt.Sprint(config.Pcb.PC), "error", err)
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa","PC",fmt.Sprint(config.Pcb.PC), "respuesta", resp.Status)
		return false
	}

	err = json.NewDecoder(resp.Body).Decode(&instruction)
	if err != nil {
		slog.Error("error al deserializar la respuesta", "error", err)
		return false
	}
	fetch = true
	return true
}

// #endregion

// #region Execute
func exec() int {

	if !fetch {
		return -1
	}

	status = 0
	bloqueante = false

	switch config.Instruccion {
	case "NOOP":

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"PC": fmt.Sprint(config.Pcb.PC),
			"Ejecutando": config.Instruccion,
		})
		config.Pcb.PC++

	case "WRITE":
		//write en la direccion del arg1 con el dato en arg2

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion + "-" + fmt.Sprint(config.Exec_values.Addr) + "-" + fmt.Sprint(config.Exec_values.Value),
		})

		status = writeMemory(config.Exec_values.Addr, config.Exec_values.Value)
		config.Pcb.PC++

	case "READ":
		//read en la direccion del arg1 con el tamaño en arg2

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion + "-" + fmt.Sprint(config.Exec_values.Addr) + "-" + fmt.Sprint(config.Exec_values.Arg1),
		})
		status = ReadMemory(config.Exec_values.Addr, config.Exec_values.Arg1)
		config.Pcb.PC++

	case "GOTO":

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion + "-" + fmt.Sprint(config.Exec_values.Arg1),
		})

		config.Pcb.PC = config.Exec_values.Arg1

	//SYSCALLS
	case "IO":
		//habilita la IO a traves de kernel

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion + "-" + fmt.Sprint(config.Exec_values.Arg1),
		})

		status = sendIO()

	case "INIT_PROC":
		//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion + "-" + config.Exec_values.Str + "-" + fmt.Sprint(config.Exec_values.Arg1),
		})

		status = initProcess()
		config.Pcb.PC++

	case "DUMP_MEMORY":
		//comprueba la memoria

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion,
		})
		status = dumpMemory()

	case "EXIT":
		//fin de proceso

		logger.RequiredLog(true, uint(config.Pcb.PID), "", map[string]string{
			"Ejecutando": config.Instruccion,
		})
		status = DeleteProcess(config.Pcb.PID)
	}

	return status
}

func writeMemory(logicAddr []int, value []byte) int {

	flag := cache.WriteMemory(logicAddr, value)

	if !flag {
		return -1
	}

	return 0
}

func ReadMemory(logicAddr []int, size int) int {

	return cache.ReadMemory(logicAddr, size)
}

// #endregion

// #region Syscalls
func sendSyscall(endpoint string, syscallInst Instruction) (*http.Response, error) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: endpoint,
		Queries: map[string]string{
			"id":  fmt.Sprint(config.Identificador),
			"pid": fmt.Sprint(config.Pcb.PID),
			"pc":  fmt.Sprint(config.Pcb.PC),
		},
	})

	// Serializar la instrucción a JSON
	jsonData, err := json.Marshal(syscallInst)
	if err != nil {
		return nil, fmt.Errorf("error al serializar instrucción: %w", err)
	}

	// Enviar el POST con el body correcto y el Content-Type
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("error al serializar instruccion: %w", err)
	}
	return resp, nil
}

func sendIO() int {

	config.Pcb.PC++   // Incrementar PC antes de enviar la syscall

	cache.EndProcess(config.Pcb.PID)

	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Error en syscall IO", "error", err)
		return -1
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error la syscall IO.", "status", resp.StatusCode)
		return -1
	}

	return -1
}

func DeleteProcess(pid int) int {

	cache.EndProcess(pid)

	config.ExitChan <- struct{}{} // aviso que hay que sacar este proceso
	return 0
}

func initProcess() int {
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para crear el proceso.", "error", err)
		return -1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al crear el proceso.", "status", resp.StatusCode)
		return -1
	}
	return 0
}

func dumpMemory() int {

	config.Pcb.PC++   // Incrementar PC antes de enviar la syscall

	cache.EndProcess(config.Pcb.PID)


	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para dump memory.", "error", err)
		return -1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al dump memory.", "status", resp.StatusCode)
		return -1
	}

	return -1
}

//#endregion

// #region kernel Connection

func notifyKernel(id string) error {
	log := slog.With("name", id)
	log.Info("Notificando a Kernel...")

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "cpu-notify",
		Queries: map[string]string{
			"ip":   httputils.GetOutboundIP(),
			"port": fmt.Sprint(config.Values.PortCPU),
			"id":   id,
		},
	})

	resp, err := http.Post(url, http.MethodPost, http.NoBody)

	if err != nil {
		fmt.Println("Probably the server is not running, logging error")
		log.Error("Error making POST request", "error", err)
		return err
	}

	if resp.StatusCode != http.StatusOK {
		log.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return fmt.Errorf("response error: %w", err)
	}

	return nil
}

func receivePIDPC() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req config.KernelResponse

		err := json.NewDecoder(r.Body).Decode(&req)
		if err != nil {
			slog.Error("Error decodificando JSON: %v", "error", err)
			http.Error(w, "JSON inválido", http.StatusBadRequest)
			return
		}

		slog.Info("Recibido desde Kernel", "PID", req.PID, " PC", req.PC)

		// Guardar la info en config global
		config.Pcb.PID = req.PID
		config.Pcb.PC = req.PC

		// Iniciar ciclo
		go ciclo()

		// Esperar que el ciclo termine y devuelva el motivo
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Proceso recibido"))
	}
}

//#endregion

//#region interrupt

func interrupt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Instance.Error("Error reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		pidRecibido, err := strconv.Atoi(string(data))

		if err != nil {
			http.Error(w, "PID invalido", http.StatusBadRequest)
			return
		}

		if pidRecibido == config.Pcb.PID {

			logger.RequiredLog(true, uint(config.Pcb.PID), "Llega interrupción al puerto Interrupt", nil)

			config.InterruptChan <- struct{}{} // Interrupción al proceso
			<-config.FinishBeforeInterrupt
			logger.RequiredLog(false,uint(config.Pcb.PID),"Se termina de atender la interrupción",nil)
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("Proceso interrumpido."))
		} else {
			http.Error(w, "PID no coincide con el proceso en ejecución", http.StatusBadRequest)
		}
	}
}

//#endregion

//#region decode

func asign() {

	switch instruction.Opcode {
	case codeutils.NOOP:
		config.Instruccion = "NOOP"
		return
	case codeutils.WRITE:
		config.Instruccion = "WRITE"
		if len(instruction.Args) != 2 {
			slog.Error("WRITE requiere 2 argumentos")
		}
		addr, _ := strconv.Atoi(instruction.Args[0])
		config.Exec_values.Addr = cache.FromIntToLogicalAddres(addr)

		bytes := []byte(instruction.Args[1])
		config.Exec_values.Value = bytes

	case codeutils.READ:
		config.Instruccion = "READ"
		if len(instruction.Args) != 2 {
			slog.Error("READ requiere 2 argumentos")
		}
		addr, _ := strconv.Atoi(instruction.Args[0])
		config.Exec_values.Addr = cache.FromIntToLogicalAddres(addr)

		//recibe int devuelve la lista de ints
		config.Exec_values.Arg1, _ = strconv.Atoi(instruction.Args[1])

	case codeutils.GOTO:
		config.Instruccion = "GOTO"
		if len(instruction.Args) != 1 {
			slog.Error("GOTO requiere 1 argumento")
		}
		arg1, err := strconv.Atoi(instruction.Args[0])
		if err != nil {
			slog.Error("error convirtiendo Valor en GOTO ", "error", err)
		}
		config.Exec_values.Arg1 = arg1

	//SYSCALLS
	case codeutils.IO:
		config.Instruccion = "IO"
		if len(instruction.Args) != 2 {
			slog.Error("IO requiere 2 argumentos")
		}
		tiempo, err := strconv.Atoi(instruction.Args[1])
		if err != nil {
			slog.Error("error convirtiendo Tiempo en IO ", "error", err)
		}
		config.Exec_values.Str = instruction.Args[0]
		config.Exec_values.Arg1 = tiempo

	case codeutils.INIT_PROC:
		config.Instruccion = "INIT_PROC"
		if len(instruction.Args) != 2 {
			slog.Error("INIT_PROC requiere 2 argumentos")
		}
		arg1, err := strconv.Atoi(instruction.Args[1])
		if err != nil {
			slog.Error("error convirtiendo Valor en INIT_PROC ", "error", err)
		}
		config.Exec_values.Str = instruction.Args[0]
		config.Exec_values.Arg1 = arg1

	case codeutils.EXIT:
		config.Instruccion = "EXIT"

	case codeutils.DUMP_MEMORY:
		config.Instruccion = "DUMP_MEMORY"
	}
}

func sendResults(pid int, pc int, motivo string) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "cpu-results",
		Queries: map[string]string{
			"pid": fmt.Sprint(pid),
			"pc": fmt.Sprint(pc),
			"reason": motivo,
		},
	})

	payload := config.DispatchResponse{
		PID:    pid,
		PC:     pc,
		Motivo: motivo,
	}

	jsonData, _ := json.Marshal(payload)
	resp, err := http.Post(url, "application/json", bytes.NewReader(jsonData))
	if err != nil {
		slog.Error("Error al enviar resultado a Kernel", "error", err)
		return
	}
	defer resp.Body.Close()
}

//#endregion
