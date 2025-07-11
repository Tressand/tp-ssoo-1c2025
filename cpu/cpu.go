package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	cache "ssoo-cpu/memory"
	"ssoo-utils/codeutils"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"sync"
	"time"
)

type Instruction = codeutils.Instruction

var instruction Instruction

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
	if !configManager.IsCompiledEnv() {
		config.Values.PortCPU += identificador
	}
	if config.Values.CacheEntries > 0 {
		cache.InitCache()
		config.CacheEnable = true
	}

	//cargar config de memoria
	cache.FindMemoryConfig()

	//crear logger
	err := logger.SetupDefault("cpu", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca CPU")

	//iniciar tlb
	cache.InitTLB(config.Values.TLBEntries, config.Values.TLBReplacement)

	//iniciar server
	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())

	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/interrupt", interrupt())
	mux.Handle("/dispatch", receivePIDPC(ctx))

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	wg.Add(1)
	go createKernelConnection(identificadorStr, &wg)

	//crear menu
	mainMenu := menu.Create()
	mainMenu.Add("Send Pid and Pc to memory", func() { sendPidPcToMemory() })
	mainMenu.Add("Close Server and Exit Program", func() {
		cancelctx()
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	mainMenu.Add("Start cicle.", func() { ciclo() })
	for {
		mainMenu.Activate()
	}
}

func ciclo() {

	for {
		slog.Info("PID: ", fmt.Sprint(config.Pcb.PID), "- FETCH - Program Counter: ", fmt.Sprint(config.Pcb.PC))

		//obtengo la intruccion (fetch)
		sendPidPcToMemory()

		//loggearla
		slog.Info("Instruccion recibida", "instruccion", fmt.Sprint(instruction))
		//decode
		asign()

		//execute
		status := exec()

		select {
		case <-config.InterruptChan:
			logger.Instance.Info("Interrupción recibida", "PID", config.Pcb.PID)
			sendResults(config.Pcb.PID, config.Pcb.PC, "Interrupt")
			return
		case <-config.ExitChan:
			logger.Instance.Info("Exit Process", "PID", config.Pcb.PID)
			sendResults(config.Pcb.PID, config.Pcb.PC, "Exit")
			return
		default:
		}

		//pequeña pausa para ver mejor el tema de los logs
		time.Sleep(1 * time.Second)

		if status == -1 {
			return
		}
	}
}

func sendPidPcToMemory() {

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"pc":  fmt.Sprint(config.Pcb.PC),
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(&instruction)
	fmt.Printf("%v", instruction)
	if err != nil {
		slog.Error("error al deserializar la respuesta", "error", err)
	}
}

// #region Execute
func exec() int{

	status := 0
	// TODO : Deberiamos mejorar el incremento de PC.
	switch config.Instruccion {
	case "NOOP":
		time.Sleep(1 * time.Millisecond)
		slog.Info("se espero 1 milisegundo por instruccion NOOP.")

	case "WRITE":
		//write en la direccion del arg1 con el dato en arg2
		writeMemory(config.Exec_values.Addr, config.Exec_values.Value)

	case "READ":
		//read en la direccion del arg1 con el tamaño en arg2
		readMemory(config.Exec_values.Addr, config.Exec_values.Arg1)

	case "GOTO":
		config.Pcb.PC = config.Exec_values.Arg1
		fmt.Printf("se actualizo el pc a %d\n", config.Exec_values.Arg1)
		fmt.Printf("PCB:\n%s", parsers.Struct(config.Pcb))

	//SYSCALLS
	case "IO":
		//habilita la IO a traves de kernel
		status = sendIO()

	case "INIT_PROC":
		//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño
		status = initProcess()

	case "DUMP_MEMORY":
		//vacia la memoria
		slog.Info("PID: ",fmt.Sprint(config.Pcb.PID)," - Ejecutando: DUMP MEMORY")
		status = dumpMemory()

	case "EXIT":
		//fin de proceso
		slog.Info("PID: ",fmt.Sprint(config.Pcb.PID)," - Ejecutando: EXIT")
		status = DeleteProcess()

	default:

	}
	if status == -1{
		return status
	}
	return 0
}

func writeMemory(logicAddr []int, value []byte) {

	cache.WriteMemory(logicAddr,value)
	config.Pcb.PC++
}

func readMemory(logicAddr []int, size int) {

	base := logicAddr[:len(logicAddr)-1]

	if cache.IsInCache(logicAddr) { //si la pagina esta en cache leo directamente

		content, flag := cache.ReadCache(base, size)

		if !flag {
			slog.Error("Error al leer la cache en la pagina ", base)
			config.ExitChan <- struct{}{}
			return
		}

		slog.Info("Contenido de direccion: ", logicAddr, " tamanio: ", size, " ", content)

	} else { //sino la busco y la leo

		fisicAddr, flag := cache.Traducir(logicAddr)

		if !flag {
			slog.Error("Error al traducir la pagina ", base)
			config.ExitChan <- struct{}{}
			return
		}

		page, _ := cache.GetPageInMemory(fisicAddr)
		cache.AddEntryCache(base, page)
		content, flag := cache.ReadCache(logicAddr, size)

		if !flag {
			slog.Error("Error al leer la cache en la pagina ", base)
			config.ExitChan <- struct{}{}
			return
		}

		slog.Info("Contenido de direccion: ", logicAddr, " tamanio: ", size, " ", content)
	}
}

// #endregion

// #region Syscalls
func sendSyscall(endpoint string, syscallInst Instruction) (*http.Response, error) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: endpoint,
		Queries: map[string]string{
			"id": fmt.Sprint(config.Identificador),
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

func sendIO() int{
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Error en syscall IO", "error", err)
		return -1
	}
	defer resp.Body.Close()
	slog.Info("Syscall IO enviada correctamente")
	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error la syscall IO.", "status", resp.StatusCode)
		return -1
	}
	config.Pcb.PC++
	return 0
}

func DeleteProcess() int{
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para eliminar proceso.", "error", err)
		return -1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al eliminar el proceso.", "status", resp.StatusCode)
		return -1
	}
	config.Pcb.PC++
	slog.Info("Kernel recibió la orden de Delete Process", "pid", config.Pcb.PID)

	cache.EndProcess(config.Pcb.PID)

	config.ExitChan <- struct{}{} // aviso que hay que sacar este proceso
	return 0
}

func initProcess() int{
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
	config.Pcb.PC++
	slog.Info("Kernel recibió la orden de init Process.", "pid", config.Pcb.PID)
	
	return 0
}

func dumpMemory() int{
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
	config.Pcb.PC++
	slog.Info("Kernel recibió la orden de dump memory.", "pid", config.Pcb.PID)

	return 0
}

//#endregion

// #region kernel Connection
func createKernelConnection(
	name string,
	wg *sync.WaitGroup, // AHORA HACE CONEXIÓN UNICA YA NO REINTENTA.
) {
	defer wg.Done()

	// Intenta conectar una sola vez
	err := notifyKernel(name)
	if err != nil {
		slog.Error("Error al notificar al Kernel", "error", err)
		return
	}

	slog.Info("Notificación al Kernel completada exitosamente")
}

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
		if resp.StatusCode == http.StatusTeapot {
			log.Info("Server asked for shutdown.")
			return nil
		}
		log.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return fmt.Errorf("response error: %w", err)
	}

	return nil
}

func receivePIDPC(ctx context.Context) http.HandlerFunc {
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

			slog.Info(" Llega interrupción al puerto Interrupt. ")

			config.InterruptChan <- struct{}{} // Interrupción al proceso
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
		config.Pcb.PC++

	case codeutils.WRITE:
		config.Instruccion = "WRITE"
		if len(instruction.Args) != 2 {
			slog.Error("WRITE requiere 2 argumentos")
		}
		config.Exec_values.Addr = cache.StringToLogicAddress(instruction.Args[0])
		
		bytes := []byte(instruction.Args[1])
		config.Exec_values.Value = bytes

	case codeutils.READ:
		config.Instruccion = "READ"
		if len(instruction.Args) != 2 {
			slog.Error("READ requiere 2 argumentos")
		}
		config.Exec_values.Addr = cache.StringToLogicAddress(instruction.Args[0])
		arg1, err1 := strconv.Atoi(instruction.Args[1])
		if err1 != nil {
			slog.Error("error convirtiendo argumentos en READ")
		}
		config.Exec_values.Arg2 = arg1

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
