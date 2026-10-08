package gateway

import (
	"strconv"
	"strings"
)

func parseCPU(data string) (total, idle uint64, ok bool) {
	line := strings.Fields(strings.SplitN(data, "\n", 2)[0])
	if len(line) < 5 || line[0] != "cpu" {
		return 0, 0, false
	}
	for i, value := range line[1:min(len(line), 9)] {
		n, err := strconv.ParseUint(value, 10, 64)
		if err != nil {
			return 0, 0, false
		}
		total += n
		if i == 3 || i == 4 {
			idle += n
		}
	}
	return total, idle, total > 0
}

func parseMemory(data string) (total, used uint64, ok bool) {
	values := map[string]uint64{}
	for _, line := range strings.Split(data, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		values[strings.TrimSuffix(f[0], ":")] = n * 1024
	}
	total = values["MemTotal"]
	available, exists := values["MemAvailable"]
	if !exists {
		available = values["MemFree"] + values["Buffers"] + values["Cached"]
	}
	return total, total - min(total, available), total > 0
}

func parseNetworks(data string) []networkSample {
	result := []networkSample{}
	for _, line := range strings.Split(data, "\n") {
		name, counters, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		if !ok || name == "lo" {
			continue
		}
		f := strings.Fields(counters)
		if len(f) < 16 {
			continue
		}
		rx, e1 := strconv.ParseUint(f[0], 10, 64)
		tx, e2 := strconv.ParseUint(f[8], 10, 64)
		if e1 == nil && e2 == nil {
			result = append(result, networkSample{Name: name, RX: rx, TX: tx})
		}
	}
	return result
}
