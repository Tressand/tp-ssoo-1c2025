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

func findFrameInMemory(logicAddr []int) (int, bool) {

	str := fromLogicAddrToString(logicAddr)

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "frame",
		Queries: map[string]string{
			"pid":     fmt.Sprint(config.Pcb.PID),
			"address": str,
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		return 0, false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status)
		return 0, false
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error al leer el cuerpo de la respuesta", "error", err)
		return 0, false
	}

	frame, err := strconv.Atoi(string(bodyBytes))
	if err != nil {
		slog.Error("error al convertir la respuesta a int", "respuesta", string(bodyBytes), "error", err)
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
		return false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa al escribir en memoria", "status", resp.Status)
		return false
	}

	var memoryConfig config.MemoryConfig
	if err := json.NewDecoder(resp.Body).Decode(&memoryConfig); err != nil {
		slog.Error("error al decodificar la respuesta de memoria", "error", err)
		return false
	}

	slog.Info("Configuración de memoria obtenida", "config", memoryConfig)
	config.MemoryConf = memoryConfig

	return true
}

func GetPageInMemory(fisicAddr []int) ([]byte, bool) {

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
		return nil, false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status)
		return nil, false
	}

	page, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error al leer la respuesta. ", "error", err)
		return nil, false
	}

	slog.Info("Cache Content", "Content:", fmt.Sprint(page))

	return page, true
}

func SavePageInMemory(page []byte, addr []int, pid int) error {
	addr_str := fmt.Sprint(addr[0])
	i := 1
	for range len(addr) - 1 {
		addr_str += "|" + fmt.Sprint(addr[i])
		i++
	}

	frame_url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "frame",
		Queries: map[string]string{
			"pid":     fmt.Sprint(config.Pcb.PID),
			"address": addr_str,
		},
	})

	frame_resp, err := http.Get(frame_url)
	if err != nil {
		return err
	}
	defer frame_resp.Body.Close()

	if frame_resp.StatusCode != http.StatusOK {
		b_err, _ := io.ReadAll(frame_resp.Body)
		slog.Error("La memoria respondió con error", "status", frame_resp.Status, "err", string(b_err))
		return err
	}

	fmt.Println(addr_str)
	frame_str, _ := io.ReadAll(frame_resp.Body)
	fmt.Println(frame_str)

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "full_page",
		Queries: map[string]string{
			"pid":  fmt.Sprint(config.Pcb.PID),
			"base": string(frame_str),
		},
	})

	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader(page))
	if err != nil {
		slog.Error("Error al hacer POST a memoria", "err", err)
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b_err, _ := io.ReadAll(resp.Body)
		slog.Error("La memoria respondió con error", "status", resp.Status, "err", string(b_err))
		return err
	}

	slog.Info("PID:", string(config.Pcb.PID), "- Memory Update - Página: ", addr, "- Frame:", string(frame_str))

	return nil
}
