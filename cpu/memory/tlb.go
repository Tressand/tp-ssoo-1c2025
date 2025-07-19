package cache

import (
	"ssoo-cpu/config"
	"time"
	"ssoo-utils/logger"
	"fmt"
)


func areSlicesEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findFrame(page []int,pid int) (int, bool) {

	for i, entry := range config.Tlb.Entries {
		if areSlicesEqual(entry.Page, page) && entry.Pid == pid{
			//tlb hit
			if config.Tlb.ReplacementAlg == "LRU" {
				config.Tlb.Entries[i].LastUsed = time.Now().UnixNano()
			}
			logger.RequiredLog(false,uint(config.Pcb.PID),"TLB HIT",map[string]string{
				"Pagina": fmt.Sprint(page),
				"Pid": fmt.Sprint(entry.Pid),
				"Frame": fmt.Sprint(entry.Frame),
			})

			for j, e := range config.Tlb.Entries {
				logger.RequiredLog(false, uint(config.Pcb.PID), fmt.Sprintf("TLB Entry #%d", j), map[string]string{
					"PID":       fmt.Sprint(e.Pid),
					"Page":      fmt.Sprint(e.Page),
					"Frame":     fmt.Sprint(e.Frame),
					"LastUsed":  fmt.Sprint(e.LastUsed),
				})
			}

			return entry.Frame, true
		}
	}

	//TLB MISS
	logger.RequiredLog(false,uint(config.Pcb.PID),"TLB MISS",map[string]string{
		"Pagina": fmt.Sprint(page),
	})
	return 0, false
}

func AddEntryTLB(page []int, frame int,pid int) {
	if config.Tlb.Capacity == 0 {
		return
	}

	if len(config.Tlb.Entries) >= config.Tlb.Capacity {
		switch config.Tlb.ReplacementAlg {
		case "FIFO":
			config.Tlb.Entries = config.Tlb.Entries[1:]
		case "LRU":
			var lruIndex int
			oldest := config.Tlb.Entries[0].LastUsed
			for i, entry := range config.Tlb.Entries {
				if entry.LastUsed < oldest {
					oldest = entry.LastUsed
					lruIndex = i
				}
			}
			config.Tlb.Entries = append(config.Tlb.Entries[:lruIndex], config.Tlb.Entries[lruIndex+1:]...)
		}

	}
	// Agregar nueva entrada con TLB vacia
	nuevoPage := make([]int, len(page))
	copy(nuevoPage, page)

	config.Tlb.Entries = append(config.Tlb.Entries, config.Tlb_entries{
		Page:     nuevoPage,
		Frame:    frame,
		LastUsed: time.Now().UnixNano(),
		Pid: pid,
	})

	logger.RequiredLog(false,uint(config.Pcb.PID),"TLB ADD",map[string]string{
		"Pagina": fmt.Sprint(page),
		"Marco": fmt.Sprint(frame),
	})
}

func InitTLB(capacity int, alg string) {
	config.Tlb.Capacity = config.Values.TLBEntries
	config.Tlb.ReplacementAlg = config.Values.TLBReplacement
	ClearTLB()
}

func ClearTLB() {
	config.Tlb.Entries = make([]config.Tlb_entries, 0, config.Tlb.Capacity)
}

func printTLB() {
	fmt.Println("----- Estado actual de la TLB -----")
	for i, entry := range config.Tlb.Entries {
		fmt.Printf("Entrada %d: Página = %v | Marco = %d | LastUsed = %d\n", i, entry.Page, entry.Frame, entry.LastUsed)
	}
	fmt.Println("-----------------------------------")
}