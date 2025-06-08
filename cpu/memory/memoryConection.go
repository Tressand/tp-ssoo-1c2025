package cache

import(
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"ssoo-utils/httputils"
	"ssoo-cpu/config"
	"strconv"
	
)

func findFrameInMemory(page []int) int{

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "user_memory",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"page": fmt.Sprint(page),
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

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("error al leer el cuerpo de la respuesta", "error", err)
		return -1
	}

	frame, err := strconv.Atoi(string(bodyBytes))
	if err != nil {
		slog.Error("error al convertir la respuesta a int", "respuesta", string(bodyBytes), "error", err)
		return -1
	}

	return frame
}

func writeMemory(fisicAddr []int, value int){

	delta := fisicAddr[1]
	frame := fisicAddr[0]

	url := httputils.BuildUrl(httputils.URLData{
		Ip:       config.Values.IpMemory,
		Port:     config.Values.PortMemory,
		Endpoint: "user_memory",
		Queries: map[string]string{
			"pid": fmt.Sprint(config.Pcb.PID),
			"frame": fmt.Sprint(frame),
			"delta": fmt.Sprint(delta),
		},
	})

	body := []byte(fmt.Sprint(value)) // convierte el int a []byte

	resp, err := http.Post(url, "text/plain", bytes.NewBuffer(body))
	if err != nil {
		slog.Error("error al realizar POST a memoria", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Error("respuesta no exitosa al escribir en memoria", "status", resp.Status)
	}
}