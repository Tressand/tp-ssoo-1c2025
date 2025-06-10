package cache

import(
	"bytes"
	"fmt"
	"io"
	"encoding/json"
	"log/slog"
	"net/http"
	"ssoo-utils/httputils"
	"ssoo-cpu/config"
	"strconv"
	
)

func findFrameInMemory(logicAddr []int) (int,bool){

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "frame",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"address": fmt.Sprint(logicAddr),
		},
	})

	resp, err := http.Get(url)
	if err != nil {
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		return 0,false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa", "respuesta", resp.Status)
		return 0,false
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error al leer el cuerpo de la respuesta", "error", err)
		return 0,false
	}

	frame, err := strconv.Atoi(string(bodyBytes))
	if err != nil {
		slog.Error("error al convertir la respuesta a int", "respuesta", string(bodyBytes), "error", err)
		return 0,false
	}

	return frame,true
}

func FindMemoryConfig() bool{
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "memory_config",
	})

	resp, err := http.Get(url)
	if err!=nil{
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


func WriteMemory(fisicAddr []int, value []byte) bool{

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "user_memory",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"base": fmt.Sprint(fisicAddr[0]),
			"delta": fmt.Sprint(fisicAddr[1]),
		},
	})

	resp, err := http.Post(url, "application/octet-stream", bytes.NewBuffer(value))
	if err != nil {
		slog.Error("error al realizar POST a memoria", "error", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa al escribir en memoria", "status", resp.Status)
		return false
	}

	slog.Info("Respuesta exitosa al escribir en memoria","fisicAddr: ",fisicAddr,"value: ",value)
	return true
}

func ReadMemory(fisicAddr []int, size int) (byte,bool){

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "user_memory",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"base": fmt.Sprint(fisicAddr[0]),
			"delta": fmt.Sprint(fisicAddr[1]),
			"size": fmt.Sprint(size),
		},
	})

	resp, err := http.Get(url)
	if err!=nil{
		slog.Error("error al realizar la solicitud a la memoria ", "error", err)
		return 0,false
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa al escribir en memoria", "status", resp.Status)
		return 0,false
	}

	var b [1]byte
	_, err = io.ReadFull(resp.Body, b[:])
	if err != nil {
		slog.Error("error al leer byte de la respuesta", "error", err)
		return 0, false
	}

	return b[0], true
}

func GetPageInMemory(fisicAddr []int) ([]byte,bool){

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "full_page",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
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
	if err != nil{
		slog.Error("error al leer la respuesta. ","error",err)
		return nil, false
	}

	return page,true
}

func SavePageInMemory(page []byte,fisicAddr []int) bool{

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "full_page",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"base": fmt.Sprint(fisicAddr[0]),
		},
	})

	resp, err := http.Post(url, "application/octet-stream", bytes.NewReader(page))
	if err != nil {
		slog.Error("Error al hacer POST a memoria", "err", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("La memoria respondió con error", "status", resp.Status)
		return false
	}

	return true
}

func NextPage(logicAddr []int)([]int,bool){
	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "nextPage",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"address": fmt.Sprint(logicAddr),
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

	var Addr []int
	if err := json.NewDecoder(resp.Body).Decode(&Addr); err != nil {
		slog.Error("error al decodificar la respuesta", "error", err)
		return nil, false
	}

	return Addr, true
}