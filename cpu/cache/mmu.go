package main

import (
	"log/slog"
	//"ssoo-cpu/config"
	"math"
)

type MMU struct{
	levels int
	pageSize uint32
	tableEntrys uint32
}


func (mmu *MMU) TraducirDireccion(logicDir uint32) {
	pageNMB := logicDir / mmu.pageSize
	scrolling := logicDir % mmu.pageSize

	slog.Info("Dirección lógica", "valor", logicDir)
	slog.Info("Tamaño de página", "valor", mmu.pageSize)
	slog.Info("Número de página", "valor", pageNMB)

	for nivel := 1; nivel <= mmu.levels; nivel++ {
		exponente := mmu.levels - nivel
		divisor := uint32(math.Pow(float64(mmu.tableEntrys), float64(exponente)))

		if divisor == 0 {
			slog.Error("Divisor en nivel %d es cero", "nivel", nivel)
			continue
		}

		entrada := (pageNMB / divisor) % mmu.tableEntrys
		slog.Info("Entrada nivel", "nivel", nivel, "valor", entrada)
	}

	slog.Info("Desplazamiento", "valor", scrolling)
}

func main() {
	// Ejemplo
	mmu := MMU{
		levels:   3,
		pageSize:     4096,
		tableEntrys: 256,
	}

	direccionLogica := uint32(12345678)
	mmu.TraducirDireccion(direccionLogica)
}