package main

import (
	"log/slog"
	"math"
)

type MMU struct{
	levels int
	pageSize uint32
	tableEntrys uint32
}

var mmu MMU

func TraducirDireccion(logicDir uint32) (uint32,bool){
	pageNMB := logicDir / mmu.pageSize
	scrolling := logicDir % mmu.pageSize

	slog.Info("Dirección lógica", "valor", logicDir)
	slog.Info("Tamaño de página", "valor", mmu.pageSize)
	slog.Info("Número de página", "valor", pageNMB)
	slog.Info("Desplazamiento", "valor", scrolling)

	frame,condition := lookupTlb(pageNMB)

	if condition{
		return frame + scrolling,true
	}else{
		var entrada uint32
		for nivel := 1; nivel <= mmu.levels; nivel++ {
			exponente := mmu.levels - nivel
			divisor := uint32(math.Pow(float64(mmu.tableEntrys), float64(exponente)))

			if divisor == 0 {
				slog.Error("Divisor en nivel %d es cero", "nivel", nivel)
				continue
			}

			entrada = (pageNMB / divisor) % mmu.tableEntrys
			slog.Info("Entrada nivel", "nivel", nivel, "entrada", entrada)
		}
		//consultar a memoria por entrada y desplazamiento el frame
		frame := consultarDireccion(entrada,scrolling)
		return frame + scrolling,true
	}
	
	return 0,false
}

func pseudomain() {

	direccionLogica := uint32(12345678)
	TraducirDireccion(direccionLogica)
}

func consultarDireccion(entrada,scrolling uint32) uint32{

	

	return 0
}