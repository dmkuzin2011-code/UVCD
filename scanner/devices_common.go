package main

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

func categoryByName(name string) (Category, bool) {
	switch name {
	case "usb":
		return usbC(), true
	case "pci":
		return pciC(), true
	case "gpu":
		return gpuC(), true
	case "monitor":
		return monitorC(), true
	case "net":
		return netC(), true
	case "disk":
		return diskC(), true
	case "cpu":
		return cpuC(), true
	case "ram":
		return ramC(), true
	case "battery":
		return batteryC(), true
	case "os":
		return osC(), true
	}
	return Category{}, false
}

func allCategories() []Category {
	return []Category{osC(), cpuC(), ramC(), gpuC(), monitorC(), usbC(), pciC(), netC(), diskC(), batteryC()}
}

func splitIP(a string) string {
	if i := strings.IndexByte(a, '/'); i >= 0 {
		return a[:i]
	}
	return a
}

// netC works on every platform through net.Interfaces().
func netC() Category {
	cat := Category{Category: "Network interfaces", Columns: []string{"Name", "MAC", "IP addresses", "State", "MTU"}}
	ifs, err := net.Interfaces()
	if err != nil {
		cat.Error = err.Error()
		return cat
	}
	var rows [][]string
	for _, inf := range ifs {
		addrs, _ := inf.Addrs()
		var ips []string
		for _, a := range addrs {
			ips = append(ips, splitIP(a.String()))
		}
		state := "down"
		if inf.Flags&net.FlagUp != 0 {
			state = "up"
		}
		if inf.Flags&net.FlagLoopback != 0 {
			state += ",loopback"
		}
		rows = append(rows, []string{inf.Name, inf.HardwareAddr.String(), strings.Join(ips, ", "), state, strconv.Itoa(inf.MTU)})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	cat.Rows, cat.Count = rows, len(rows)
	return cat
}
