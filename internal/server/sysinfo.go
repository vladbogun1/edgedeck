package server

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Данные о сервере читаем из /proc хоста, а не своего: панель в контейнере, и
// её собственные цифры пользователю неинтересны.

func hostname(procDir string) string {
	if b, err := os.ReadFile(filepath.Join(procDir, "sys", "kernel", "hostname")); err == nil {
		return strings.TrimSpace(string(b))
	}
	if h, err := os.Hostname(); err == nil {
		return h
	}
	return "сервер"
}

func fillSystem(info *serverInfo, procDir, diskPath string) {
	info.LoadAvg = loadAvg(procDir)
	info.MemTotalMB, info.MemUsedMB = memory(procDir)
	info.CPUCount = cpuCount(procDir)
	info.DiskTotalGB, info.DiskUsedGB = diskUsage(diskPath)
}

func loadAvg(procDir string) float64 {
	b, err := os.ReadFile(filepath.Join(procDir, "loadavg"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// memory считает «занято» так же, как free(1): total минус available.
// MemFree врёт, потому что кеш страниц формально занят, но отдаётся по требованию.
func memory(procDir string) (totalMB, usedMB int) {
	f, err := os.Open(filepath.Join(procDir, "meminfo"))
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	var total, available int
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		v, err := strconv.Atoi(fields[1]) // в килобайтах
		if err != nil {
			continue
		}
		switch fields[0] {
		case "MemTotal:":
			total = v
		case "MemAvailable:":
			available = v
		}
	}
	if total == 0 {
		return 0, 0
	}
	return total / 1024, (total - available) / 1024
}

func cpuCount(procDir string) int {
	b, err := os.ReadFile(filepath.Join(procDir, "cpuinfo"))
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "processor") {
			n++
		}
	}
	return n
}
