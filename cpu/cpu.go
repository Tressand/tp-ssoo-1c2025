package main

import (
	"strings"
	"strconv"
	"time"
	"fmt"
	"ssoo-cpu/config"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	asign("IO 8")
	exec()
}

func exec(){
	switch config.Instruccion{
		case "NOOP":
			time.Sleep(1 * time.Second)
			fmt.Println("se espero 1 segundo")
			//no hace nada

		case "WRITE":
			//write en la direccion del arg1 con el dato en arg2

		case "READ":
			//read en la direccion del arg1 con el tamaño en arg2

		case "GOTO":
			config.Pcb.PC = config.Exec_values.Arg1
			fmt.Printf("se actualizo el pc a %d\n",config.Exec_values.Arg1)
			fmt.Printf("PCB:\n%s", parsers.Struct(config.Pcb))

		case "IO":
			time.Sleep(time.Second * time.Duration(config.Exec_values.Arg1))
			fmt.Printf("se espero %d segundo\n",config.Exec_values.Arg1)
			//simula una IO por un tiempo igual al arg1

		case "INIT_PROC":
			//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño

		case "DUMP_MEMORY":
			//vacia la memoria

		case "EXIT":
			//fin de proceso

		default:
			
	}
	//pcb.pc ++
}

func asign(bruto string){
	partes := strings.Fields(bruto)

	if len(partes)==0{
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
}