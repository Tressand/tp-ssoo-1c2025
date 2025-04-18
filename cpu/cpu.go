package main

import (
	"strings"
	"fmt"
	"ssoo-cpu/config"
	"ssoo-utils/parsers"
)

func main() {
	config.Load()
	fmt.Printf("Config Loaded:\n%s", parsers.Struct(config.Values))
	asign("WRITE 1 2")
	asign("EXIT")
}

func exec(instruccion string){
	switch(instruccion){
		case "NOOP":
			//no hace nada
			break;
		case "WRITE":
			//write en la direccion del arg1 con el dato en arg2
			break;
		case "READ":
			//read en la direccion del arg1 con el tamaño en arg2
			break;
		case "GOTO":
			//actualizar el pc en pcb por el arg1
			break;
		case "IO":
			//WAIT(ARG1)
			break;
		case "INIT_PROC":
			//inicia un proceso con el arg1 como el arch de instrc. y el arg2 como el tamaño
			break;
		case "DUMP_MEMORY":
			//vacia la memoria
			break;
		case "EXIT":
			//
			break;
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
	
	funcion := partes[0]
	var arg1,arg2 string

	if len(partes) > 1 {
		arg1 = partes[1]
	}
	if len(partes) > 2 {
		arg2 = partes[2]
	}

	fmt.Println("Función:", funcion)
	if arg1 != "" {
		fmt.Println("Argumento 1:", arg1)
	}
	if arg2 != "" {
		fmt.Println("Argumento 2:", arg2)
	}
}