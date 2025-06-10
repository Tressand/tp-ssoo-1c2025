package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"ssoo-cpu/config"
	"ssoo-cpu/memory"
	"ssoo-utils/codeutils"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	"strings"
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
	if(config.Values.CacheEntries > 0){
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
		slog.Info("Inicio de ciclo de instrucción", "PID", config.Pcb.PID, "PC", config.Pcb.PC)

		//obtengo la intruccion (fetch)
		sendPidPcToMemory()

		//loggearla
		slog.Info("Instruccion recibida", "instruccion", fmt.Sprint(instruction))
		//decode
		asign()

		//execute
		exec()

		select {
		case <-config.InterruptChan:
			logger.Instance.Info("Interrupción recibida", "PID", config.Pcb.PID)
			config.CicloDone <- "Interrupt"
			return
		case <-config.ExitChan:
			logger.Instance.Info("Exit Process", "PID", config.Pcb.PID)
			config.CicloDone <- "Exit"
			return
		default:
		}

		//pequeña pausa para ver mejor el tema de los logs
		time.Sleep(1 * time.Second)
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

	// Deserializa la respuesta JSON a un objeto Instruction
	err = json.NewDecoder(resp.Body).Decode(&instruction)
	fmt.Printf("%v", instruction)
	if err != nil {
		slog.Error("error al deserializar la respuesta", "error", err)
	}
	// Devuelve la instrucción obtenida
	//return &instruction, nil
	//falta ver que se hacen con los datos enviados por memoria en response.
	//log.Printf("Instrucciones recibidas: %v", response.Instrucciones) //dejo esto por q no se que me trae todavia
}

// #region Execute
func exec() {

	switch config.Instruccion {
	case "NOOP":
		time.Sleep(1 * time.Millisecond)
		slog.Info("se espero 1 milisegundo por instruccion NOOP.")

	case "WRITE":
		//write en la direccion del arg1 con el dato en arg2
		writeMemory(config.Exec_values.Addr,config.Exec_values.Value)

	case "READ":
		//read en la direccion del arg1 con el tamaño en arg2
		readMemory(config.Exec_values.Addr,config.Exec_values.Arg1)

	case "GOTO":
		config.Pcb.PC = config.Exec_values.Arg1
		fmt.Printf("se actualizo el pc a %d\n", config.Exec_values.Arg1)
		fmt.Printf("PCB:\n%s", parsers.Struct(config.Pcb))
		return

	//SYSCALLS
	case "IO":
		//habilita la IO a traves de kernel
		sendIO()

	case "INIT_PROC":
		//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño
		initProcess()

	case "DUMP_MEMORY":
		//vacia la memoria
		slog.Info("DUMP_MEMORY Instruction not implemented.")
		dumpMemory()

	case "EXIT":
		//fin de proceso
		DeleteProcess()

	default:

	}
	config.Pcb.PC++
}

func writeMemory(logicAddr []int, value []byte){

	if cache.IsInCache(logicAddr){
		
	}

	fisicAddr := cache.Traducir(logicAddr)
	cache.WriteMemory(fisicAddr,value)
}

func readMemory(logicAddr []int, size int){

	base := logicAddr[:len(logicAddr)-1]

	if cache.IsInCache(logicAddr){
		
		content := cache.ReadCache(base,size)
		slog.Info("Contenido de direccion: ",logicAddr," tamanio: ",size, " ",content)

	}else{
		fisicAddr := cache.Traducir(logicAddr)
		page,_ := cache.GetPageInMemory(fisicAddr)
		cache.AddEntryCache(base,page)
		content := cache.ReadCache(logicAddr,size)
		
		slog.Info("Contenido de direccion: ",logicAddr," tamanio: ",size, " ",content)
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

	resp, err := http.Post(url,http.MethodPost,http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("error al serializar instruccion: %w", err)
	}


	return resp, nil
}

func sendIO() {
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Error en syscall IO", "error", err)
		return
	}
	defer resp.Body.Close()
	slog.Info("Syscall IO enviada correctamente")
	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al eliminar el proceso.", "status", resp.StatusCode)
		return
	}
}

func DeleteProcess() {
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para eliminar proceso.", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al eliminar el proceso.", "status", resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de Delete Process", "pid", config.Pcb.PID)
	config.ExitChan <- "" // aviso que hay que sacar este proceso
}

func initProcess() {
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para crear el proceso.", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al crear el proceso.", "status", resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de init Process.", "pid", config.Pcb.PID)
}

func dumpMemory(){
	resp, err := sendSyscall("syscall", instruction)
	if err != nil {
		slog.Error("Fallo la solicitud para dump memory.", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al dump memory.", "status", resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de dump memory.", "pid", config.Pcb.PID)
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
		select {
		case motivo := <-config.CicloDone:
			resp := config.DispatchResponse{
				PID:    req.PID,
				PC:     req.PC, // o el valor actualizado si cambia durante el ciclo
				Motivo: motivo,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)

		case <-ctx.Done():
			http.Error(w, "Contexto cancelado", http.StatusInternalServerError)
		}
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
			config.InterruptChan <- "" // Interrupción al proceso
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

	case codeutils.WRITE:
		config.Instruccion = "WRITE"
		if len(instruction.Args) != 2 {
			slog.Error("WRITE requiere 2 argumentos")
		}
		config.Exec_values.Addr = cache.StringToLogicAddress(instruction.Args[0])
		
		parts := strings.Split(instruction.Args[1], "|")
		bytes := make([]byte, len(parts))
		for i, s := range parts {
			val, err := strconv.Atoi(s)
			if err != nil {
				slog.Error("error al convertir '%s' a byte: %v", s, err)
				return
			}
			if val < 0 || val > 255 {
				slog.Error("valor fuera de rango para byte: %d", val)
				return
			}
			bytes[i] = byte(val)
		}
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

//#endregion
