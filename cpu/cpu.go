package main

import (
	//"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	//"log"
	"log/slog"
	"net/http"
	//"net/url"
	"os"
	"ssoo-cpu/config"
	"ssoo-utils/codeutils"
	"ssoo-utils/configManager"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/menu"
	"ssoo-utils/parsers"
	"strconv"
	//"strings"
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
	initTLB(config.Values.TLBEntries,config.Values.TLBReplacement)

	//iniciar server
	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())

	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/interrupt", interrupt())
	mux.Handle("/dispatch", receivePIDPC(ctx))

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	wg.Add(1)
	go createKernelConnection("CPU_"+identificadorStr, 3, 5, &wg, ctx)
	


	//crear menu
	mainMenu := menu.Create()
	mainMenu.Add("Store value on Memory", func() { sendValueToMemory(getInput()) })
	mainMenu.Add("Send Pid and Pc to memory", func() {sendPidPcToMemory()})
	mainMenu.Add("Close Server and Exit Program", func() {
		cancelctx()
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	mainMenu.Add("Start cicle.",func(){ciclo()})
	for {
		mainMenu.Activate()
	}
}

func ciclo(){
	

	for{
		slog.Info("Inicio de ciclo de instrucción","PID",config.Pcb.PID, "PC", config.Pcb.PC)

		//obtengo la intruccion (fetch)
		sendPidPcToMemory()

		//loggearla
		slog.Info("Instruccion:", fmt.Sprint(instruction))
		//decode
		asign()

		//execute
		exec()

		select{
			case <-config.InterruptChan:
				logger.Instance.Info("Interrupción recibida","PID", config.Pcb.PID)
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

func sendValueToMemory(key string, value string) {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "storage",
		Queries: map[string]string{
			"key":   key,
			"value": value,
		},
	})

	fmt.Printf("Connecting to %s\n", url)
	resp, err := http.Post(url, http.MethodPost, http.NoBody)
	if err != nil {
		slog.Error("POST to Memory failed", "Error", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("POST to Memory status wrong", "status", resp.StatusCode)
		return
	}

	slog.Info("POST to Memory succeded")
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
	if err != nil {
		slog.Error("error al deserializar la respuesta", "error", err)
	}
	// Devuelve la instrucción obtenida
	//return &instruction, nil
	//falta ver que se hacen con los datos enviados por memoria en response.
	//log.Printf("Instrucciones recibidas: %v", response.Instrucciones) //dejo esto por q no se que me trae todavia
}

//#region Execute
func exec() {
	switch config.Instruccion {
	case "NOOP":
		time.Sleep(1 * time.Millisecond)
		slog.Info("se espero 1 milisegundo por instruccion NOOP.")

	case "WRITE":
		//write en la direccion del arg1 con el dato en arg2
		writeMemory()

	case "READ":
		//read en la direccion del arg1 con el tamaño en arg2
		readMemory()

	case "GOTO":
		config.Pcb.PC = config.Exec_values.Arg1
		fmt.Printf("se actualizo el pc a %d\n", config.Exec_values.Arg1)
		fmt.Printf("PCB:\n%s", parsers.Struct(config.Pcb))
		return
	
	//SYSCALLS
	case "IO":
		//habilita la IO a traves de kernel
		sendIO();

	case "INIT_PROC":
		//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño
		initProcess()

	case "DUMP_MEMORY":
		//vacia la memoria
		dumpMemory()

	case "EXIT":
		//fin de proceso
		DeleteProcess()

	default:

	}
	config.Pcb.PC++
}



func readMemory(){

	//TODO HALLAR LA DIRECCION FISICA A PARTIR DE LA DIRECCION LOGICA

	var dir_fisica = 10210

	//parte HTTP

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"direction": fmt.Sprint(dir_fisica),
			"size": fmt.Sprint(config.Exec_values.Arg1),
		},
	})

	resp, err := http.Get(url)

	if err != nil {
		slog.Error("Error al solicitar el dato de memoria. ", "error",err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("memoria respondió con error","respuesta", resp.Status)
		return
	}

	var result struct {
		Contenido string `json:"contenido"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		slog.Error("error al decodificar respuesta de memoria: %w", err)
		return
	}

	fmt.Printf("El dato en la direccion es: %s ",result.Contenido)
	slog.Info("El dato en la direccion es ","dato",result.Contenido)
}

func writeMemory(){

	//TODO HALLAR LA DIRECCION FISICA A PARTIR DE LA DIRECCION LOGICA

	var dir_fisica = 10210

	//parte HTTP

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"direction": fmt.Sprint(dir_fisica),
			"size": fmt.Sprint(config.Exec_values.Arg1),
		},
	})

	resp, err := http.Post(url,http.MethodPost,http.NoBody)

	if err != nil {
		slog.Error("Error al solicitar el dato de memoria. ", "error",err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("memoria respondió con error ","error", resp.Status)
		return
	}

	slog.Info("Se ha guardado el contenido exitosamente.")
}

//#endregion
//#region Syscalls

func sendIO(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "syscall",
		Queries: map[string]string{
			"instruccion": fmt.Sprint(instruction),
		},
	})

	resp, err := http.Post(url,http.MethodPost,http.NoBody)
	if err != nil {
		slog.Error("Error al crear la solicitud IO", "error", err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al recibir IO. ", "status", resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de IO", "pid", config.Pcb.PID)
}

func DeleteProcess(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "syscall",
		Queries: map[string]string{
			"instruccion": fmt.Sprint(instruction),
		},
	})

	resp, err := http.Post(url,http.MethodPost,http.NoBody)

	if err != nil {
		slog.Error("Fallo la solicitud para eliminar proceso. ", "error", err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Kernel respondió con error al eliminar el proceso. ", "status", resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de Delete Process", "pid", config.Pcb.PID)
	config.ExitChan <- "" //aviso que hay que sacar este proceso
}

func initProcess(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "syscall",
		Queries: map[string]string{
			"instruccion": fmt.Sprint(instruction),
		},
	})

	resp,err := http.Post(url,http.MethodPost,http.NoBody)
	
	if err != nil{
		slog.Error("Fallo la solicitud para crear el proceso. ","error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK{

		slog.Error("Kernel respondió con error al crear el proceso. ","status",resp.StatusCode)
		return
	}

	slog.Info("Kernel recibió la orden de init Process. ","pid",config.Pcb.PID)
}

func dumpMemory(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpKernel,
		Port:     config.Values.PortKernel,
		Endpoint: "syscall",
		Queries: map[string]string{
			"instruccion": fmt.Sprint(instruction),
		},
	})

	resp, err := http.Post(url,http.MethodPost,http.NoBody)

	if err != nil {
		slog.Error("Error al solicitar el vaciado de la memoria. ", "error",err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("memoria respondió con error ", "error", resp.Status)
		return
	}

	slog.Info("Se ha borrado la memoria. ")

}


//#endregion

//#region kernel Connection

func createKernelConnection(
	name string,
	retryAmount int,
	retrySeconds int,
	wg *sync.WaitGroup,
	ctx context.Context) {
	defer wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		default:
			retry, err := notifyKernel(name, ctx)
			if !retry {
				return
			}
			if err != nil {
				if retryAmount <= 0 {
					return
				}
				time.Sleep(time.Duration(retrySeconds) * time.Second)
				retryAmount--
			}
		}
	}
}

func notifyKernel(id string, ctx context.Context) (bool, error) {
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
		return true, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTeapot {
			log.Info("Server asked for shutdown.")
			return false, nil
		}
		log.Error("Error on response", "Status", resp.StatusCode, "error", err)
		return true, fmt.Errorf("response error: %w", err)
	}

	data, _ := io.ReadAll(resp.Body)
	duration, _ := strconv.Atoi(string(data))
	log.Info("Recibió respuesta, durmiendo...", "timer", duration)

	sleepDone := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(duration) * time.Millisecond)
		sleepDone <- struct{}{}
		fmt.Println("sleep goroutine closed")
	}()
	defer close(sleepDone)

	select {
	case <-sleepDone:
		return true, nil
	case <-ctx.Done():
		return false, nil
	}
}

func receivePIDPC(ctx context.Context) http.HandlerFunc{
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var req config.KernelResponse

		err := json.NewDecoder(r.Body).Decode(&req);
		if err != nil {
			logger.Instance.Error("Error decodificando JSON: %v", err)
			http.Error(w, "JSON inválido", http.StatusBadRequest)
			return
		}

		slog.Info("Recibido desde Kernel: PID: ",req.PID," PC: ", req.PC)

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

		pidStr := string(data)
		pidRecibido, err := strconv.Atoi(pidStr)
		
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

func getInput() (string, string) {
	fmt.Print("Key: ")
	var key string
	var value string
	fmt.Scanln(&key)
	fmt.Print("Value: ")
	fmt.Scanln(&value)

	return key, value
}

//#region decode

func asign(){

	switch(instruction.Opcode){
		case codeutils.NOOP:
			config.Instruccion= "NOOP"

		case codeutils.WRITE:
			config.Instruccion = "WRITE"
			if len(instruction.Args) != 2 {
				slog.Error("WRITE requiere 2 argumentos")
			}
			arg1, err := strconv.Atoi(instruction.Args[0])
			if err != nil {
				slog.Error("error convirtiendo Dirección en WRITE ","error", err)
			}
			config.Exec_values.Arg1 = arg1
			config.Exec_values.Str = instruction.Args[1]
		
		case codeutils.READ:
			config.Instruccion = "READ"
			if len(instruction.Args) != 2 {
				slog.Error("READ requiere 2 argumentos")
			}
			arg1, err1 := strconv.Atoi(instruction.Args[0])
			arg2, err2 := strconv.Atoi(instruction.Args[1])
			if err1 != nil || err2 != nil {
				slog.Error("error convirtiendo argumentos en READ")
			}
			config.Exec_values.Arg1 = arg1
			config.Exec_values.Arg2 = arg2
		
		case codeutils.GOTO:
			config.Instruccion = "GOTO"
			if len(instruction.Args) != 1 {
				slog.Error("GOTO requiere 1 argumento")
			}
			arg1, err := strconv.Atoi(instruction.Args[0])
			if err != nil {
				slog.Error("error convirtiendo Valor en GOTO ","error", err)
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
				slog.Error("error convirtiendo Tiempo en IO ","error", err)
			}
			config.Exec_values.Str = instruction.Args[0]
			config.Exec_values.Arg1 = tiempo
		
		case codeutils.INIT_PROC:
			config.Instruccion = "INIT_PROC"
			if len(instruction.Args) != 2{
				slog.Error("INIT_PROC requiere 2 argumentos")
			}
			arg1, err := strconv.Atoi(instruction.Args[1])
			if err != nil {
				slog.Error("error convirtiendo Valor en INIT_PROC ","error", err)
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