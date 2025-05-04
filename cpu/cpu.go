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
	"os"
	"ssoo-cpu/config"
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

	//Ejecucion de practica
	//asign("IO 8")
	//exec()

	//crear logger
	err := logger.SetupDefault("cpu", config.Values.LogLevel)
	defer logger.Close()
	if err != nil {
		fmt.Printf("Error setting up logger: %v\n", err)
		return
	}
	log := logger.Instance
	log.Info("Arranca CPU")

	//iniciar server
	var mux *http.ServeMux = http.NewServeMux()

	mux.Handle("/interrupt", interrupt())

	shutdownSignal := make(chan any)
	httputils.StartHTTPServer(httputils.GetOutboundIP(), config.Values.PortCPU, mux, shutdownSignal)

	var wg sync.WaitGroup
	ctx, cancelctx := context.WithCancel(context.Background())

	wg.Add(1)
	go createKernelConnection("CPU1", 3, 5, &wg, ctx)

	//crear menu
	mainMenu := menu.Create()
	mainMenu.Add("Store value on Memory", func() { sendValueToMemory(getInput()) })
	mainMenu.add("Send Pid and Pc to memory", func() {sendPidPcToMemory()})
	mainMenu.Add("Close Server and Exit Program", func() {
		cancelctx()
		shutdownSignal <- struct{}{}
		<-shutdownSignal
		close(shutdownSignal)
		os.Exit(0)
	})
	for {
		mainMenu.Activate()
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
		fmt.Errorf("error al realizar la solicitud a la memoria: %v", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Errorf("respuesta no exitosa: %s", resp.Status)
	}

	// Deserializa la respuesta JSON a un objeto Instruction
	err = json.NewDecoder(resp.Body).Decode(&instruction)
	if err != nil {
		fmt.Errorf("error al deserializar la respuesta: %v", err)
	}
	// Devuelve la instrucción obtenida
	//return &instruction, nil
	//falta ver que se hacen con los datos enviados por memoria en response.
	//log.Printf("Instrucciones recibidas: %v", response.Instrucciones) //dejo esto por q no se que me trae todavia
}

func DeleteProcess(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
		},
	})

	req, err := http.NewRequest(http.MethodDelete,url,nil)
	if err != nil {
		slog.Error("Error al crear la solicitud DELETE", "error", err)
		return
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Fallo la solicitud para eliminar proceso. ", "error", err)
		return
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("Memoria respondió con error al eliminar el proceso. ", "status", resp.StatusCode)
		return
	}

	slog.Info("Proceso eliminado exitosamente en Memoria", "pid", config.Pcb.PID)
}

func initProcess(){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "process",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"name": config.Exec_values.Str,
		},
	})

	resp,err := http.Post(url,http.MethodPost,http.NoBody)

	if err != nil{
		slog.Error("Fallo la solicitud para crear el proceso. ","error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK{

		slog.Error("Memoria respondió con error al crear el proceso. ","status",resp.StatusCode)
		return
	}

	slog.Info("Proceso creado exitosamente en Memoria. ","pid",config.Pcb.PID)
}

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

func interrupt() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Interruptions not implemented

		data, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Instance.Error("Error reading request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		message := string(data)
		logger.Instance.Info("Received message from kernel", "message", message)

		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Message received."))
	}
}

func getInput() (string, string) {
	fmt.Print("Key: ")
	var key string
	var value string
	fmt.Scanln(&key)
	fmt.Print("Value: ")
	fmt.Scanln(&value)

	return key, value
}

func exec() {
	switch config.Instruccion {
	case "NOOP":
		time.Sleep(1 * time.Millisecond)
		fmt.Println("se espero 1 milisegundo.")
		//no hace nada
		//TODO
	case "WRITE":
		//write en la direccion del arg1 con el dato en arg2
		//TODO
	case "READ":
		//read en la direccion del arg1 con el tamaño en arg2
		//TODO
	case "GOTO":
		config.Pcb.PC = config.Exec_values.Arg1
		fmt.Printf("se actualizo el pc a %d\n", config.Exec_values.Arg1)
		fmt.Printf("PCB:\n%s", parsers.Struct(config.Pcb))
		return
	
	//SYSCALLS
	case "IO":
		time.Sleep(time.Millisecond * time.Duration(config.Exec_values.Arg1))
		fmt.Printf("se espero %d milisegundo.\n", config.Exec_values.Arg1)
		//simula una IO por un tiempo igual al arg1
		//TODO
	case "INIT_PROC":
		//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño
		initProcess()
	case "DUMP_MEMORY":
		//vacia la memoria
		//TODO
	case "EXIT":
		//fin de proceso
		DeleteProcess()
	default:

	}
	config.Pcb.PC++
}
/*
func asign(bruto string) {

	partes := strings.Fields(bruto)

	if len(partes) == 0 {
		fmt.Println("Cadena vacía o sin funcion")
		return
	}

	config.Instruccion = partes[0]

	if len(partes) > 1 {
		val, err := strconv.Atoi(partes[1])
		if err != nil {
			fmt.Println("Error convirtiendo arg1 a int:", err)
		} else {
			config.Exec_values.Arg1 = val
		}
	}
	if len(partes) > 2 {
		val, err := strconv.Atoi(partes[2])
		if err != nil {
			fmt.Println("Error convirtiendo arg2 a int:", err)
		} else {
			config.Exec_values.Arg2 = val
		}
	}

	fmt.Println("Función:", config.Instruccion)
	if config.Exec_values.Arg1 != -1 {
		fmt.Println("Argumento 1:", config.Exec_values.Arg1)
	}
	if config.Exec_values.Arg2 != -1 {
		fmt.Println("Argumento 2:", config.Exec_values.Arg2)
	}
}*/

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
				slog.Error("error convirtiendo Dirección en WRITE: %v", err)
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
				slog.Error("error convirtiendo Valor en GOTO: %v", err)
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
				slog.Error("error convirtiendo Tiempo en IO: %v", err)
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
				slog.Error("error convirtiendo Valor en INIT_PROC: %v", err)
			}
			config.Exec_values.Str = instruction.Args[0]
			config.Exec_values.Arg1 = arg1
		
		case codeutils.EXIT:
			config.Instruccion = "EXIT"
		
		case codeutils.DUMP_MEMORY:
			config.Instruccion = "DUMP_MEMORY"
	}	
}