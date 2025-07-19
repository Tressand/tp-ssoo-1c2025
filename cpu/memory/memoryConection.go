package cache

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-cpu/config"
	"ssoo-utils/httputils"
	"ssoo-utils/logger"
	"ssoo-utils/parsers"
	"strconv"
	"strings"
)

func fromLogicAddrToString(logicAddr []int) string {
	strs := make([]string, len(logicAddr))

	for i, num := range logicAddr {
		strs[i] = strconv.Itoa(num)
	}

	return strings.Join(strs, "|")
}

func findFrameInMemory(logicAddr []int,pid int) (int, bool) {

	str := fromLogicAddrToString(logicAddr)

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "frame",
		Queries: map[string]string{
			"pid":     fmt.Sprint(pid),
			"address": str,
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status)
		MandarDumpMemory(config.Pcb.PID)
		return 0, false
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error al leer el cuerpo de la respuesta", "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return 0, false
	}

	frame, err := strconv.Atoi(string(bodyBytes))
	if err != nil {
		slog.Error("error al convertir la respuesta a int", "respuesta", string(bodyBytes), "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return 0, false
	}

	return frame, true
}

func FindMemoryConfig() bool {
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "memory_config",
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa al escribir en memoria", "status", resp.Status)
		MandarDumpMemory(config.Pcb.PID)
		return false
	}

	var memoryConfig config.PaginationConfig
	if err := json.NewDecoder(resp.Body).Decode(&memoryConfig); err != nil {
		slog.Error("error al decodificar la respuesta de memoria", "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return false
	}

	config.MemoryConf = memoryConfig
	fmt.Println("Configuración de memoria obtenida:\n", parsers.Struct(config.MemoryConf))

	return true
}

func GetPageInMemory(fisicAddr []int, logicAddr []int) ([]byte, bool) {

	logger.RequiredLog(false, uint(config.Pcb.PID), "OBTENER MARCO", map[string]string{
		"Pagina": fmt.Sprint(logicAddr),
		"Marco":  fmt.Sprint(fisicAddr[0]),
	})

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "full_page",
		Queries: map[string]string{
			"pid":  fmt.Sprint(config.Pcb.PID),
			"base": fmt.Sprint(fisicAddr[0]),
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		MandarDumpMemory(config.Pcb.PID)
		return nil, false
	}

	defer resp.Body.Close()
	page,_ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status, "error", page)
		MandarDumpMemory(config.Pcb.PID)
		return nil, false
	}

	return page, true
}

func SavePageInMemory(page []byte, addr []int, pid int) error {

	if pid == -1{
		return nil
	}

	if config.CacheEnable{
		logger.RequiredLog(false,uint(config.Pcb.PID)," Saving Cache",map[string]string{
			"Pagina": fmt.Sprint(addr),
			"PID": fmt.Sprint(pid),
		})	
	}

	addr = append(addr, 0)

	frame_str, _ := Traducir(addr,pid)

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "full_page",
		Queries: map[string]string{
			"pid":  fmt.Sprint(pid),
			"base": fmt.Sprint(frame_str[0]),
		},
	})

	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader(page))
	if err != nil {
		slog.Error("Error al hacer POST a memoria", "err", err)
		MandarDumpMemory(pid)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b_err, _ := io.ReadAll(resp.Body)
		slog.Error("La memoria respondió con error", "status", resp.Status, "err", string(b_err))
		MandarDumpMemory(pid)
		return err
	}

	return nil
}

func MandarDumpMemory(pid int){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "memory_dump",
		Queries: map[string]string{
			"pid":  fmt.Sprint(pid),
		},
	})

	resp, err := http.Post(url, "application/octet-stream", nil)
	if err != nil {
		slog.Error("Error al hacer POST a memoria", "err", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b_err, _ := io.ReadAll(resp.Body)
		slog.Error("La memoria respondió con error", "status", resp.Status, "err", string(b_err))
	}
}