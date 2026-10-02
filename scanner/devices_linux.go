//go:build linux

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

func fileGet(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func readBytes(p string) []byte {
	b, _ := os.ReadFile(p)
	return b
}

// ---------- USB ----------

func usbC() Category {
	cat := Category{Category: "USB devices", Columns: []string{"Port", "Vendor:Product", "Product", "Manufacturer", "Serial", "Speed"}}
	dirs, err := os.ReadDir("/sys/bus/usb/devices")
	if err != nil {
		cat.Error = "cannot read /sys/bus/usb/devices: " + err.Error()
		return cat
	}
	var rows [][]string
	for _, d := range dirs {
		name := d.Name()
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue // interface dirs (1-0:1.0) and root hubs (usb1)
		}
		base := "/sys/bus/usb/devices/" + name + "/"
		if fileGet(base+"idVendor") == "" {
			continue
		}
		speed := fileGet(base + "speed")
		if n, err := strconv.Atoi(speed); err == nil {
			speed = fmt.Sprintf("%d Mbps", n)
		}
		rows = append(rows, []string{
			name,
			fileGet(base+"idVendor") + ":" + fileGet(base+"idProduct"),
			fileGet(base + "product"),
			fileGet(base + "manufacturer"),
			fileGet(base + "serial"),
			speed,
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	cat.Rows, cat.Count = rows, len(rows)
	return cat
}

// ---------- PCI ----------

var vendorNames = map[string]string{
	"0x8086": "Intel",
	"0x10de": "NVIDIA",
	"0x1002": "AMD",
	"0x1022": "AMD",
	"0x14e4": "Broadcom",
	"0x10ec": "Realtek",
	"0x1d6b": "Linux Foundation",
	"0x8087": "Intel",
	"0x1969": "Atheros",
	"0x168c": "Qualcomm",
	"0x1106": "VIA",
	"0x15ad": "VMware",
	"0x1af4": "Virtio",
}

var pciClasses = map[string]string{
	"00": "Unclassified", "01": "Mass storage", "02": "Network", "03": "Display",
	"04": "Multimedia", "05": "Memory", "06": "Bridge", "07": "Communication",
	"08": "System peripheral", "09": "Input device", "0a": "Docking station",
	"0b": "Processor", "0c": "Serial bus", "0d": "Wireless", "0e": "Intelligent I/O",
	"0f": "Satellite", "10": "Encryption", "11": "Signal processing",
	"12": "Processing accelerator", "13": "Non-essential", "40": "Co-processor", "ff": "Unassigned",
}

var pciSubs = map[string]string{
	"0106": "SATA", "0108": "NVMe", "0105": "IDE", "0104": "RAID", "0102": "Floppy",
	"0200": "Ethernet", "0300": "VGA-compatible", "0302": "3D controller",
	"0401": "Audio", "0600": "Host bridge", "0601": "ISA bridge", "0604": "PCI-to-PCI",
	"0c03": "USB controller", "0c00": "FireWire", "0c05": "SMBus", "0c04": "HID",
	"0d03": "Bluetooth", "0d00": "Wi-Fi",
}

func vendorName(id string) string {
	id = strings.ToLower(id)
	if v, ok := vendorNames[id]; ok {
		return v
	}
	return id
}

func pciClassDesc(classHex string) string {
	c := strings.TrimPrefix(classHex, "0x")
	if len(c) < 4 {
		if len(c) == 2 {
			if n, ok := pciClasses[c]; ok {
				return n
			}
		}
		return classHex
	}
	name := pciClasses[c[:2]]
	if name == "" {
		name = "Class 0x" + c[:2]
	}
	if n, ok := pciSubs[c[:4]]; ok {
		name += " - " + n
	}
	return name
}

func pciRows(onlyDisplay bool) [][]string {
	var rows [][]string
	dirs, err := os.ReadDir("/sys/bus/pci/devices")
	if err != nil {
		return rows
	}
	for _, d := range dirs {
		addr := d.Name()
		base := "/sys/bus/pci/devices/" + addr + "/"
		vID := fileGet(base + "vendor")
		dID := fileGet(base + "device")
		if vID == "" {
			continue
		}
		if onlyDisplay && !strings.HasPrefix(strings.TrimPrefix(fileGet(base+"class"), "0x"), "03") {
			continue
		}
		driver := ""
		if link, err := os.Readlink(base + "driver"); err == nil {
			driver = filepath.Base(link)
		}
		if onlyDisplay {
			rows = append(rows, []string{addr, pciClassDesc(fileGet(base + "class")), vendorName(vID), dID, driver})
		} else {
			rows = append(rows, []string{addr, pciClassDesc(fileGet(base + "class")), vendorName(vID), vID, dID, driver})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func pciC() Category {
	cat := Category{Category: "PCI devices", Columns: []string{"Address", "Description", "Vendor", "Vendor ID", "Device ID", "Driver"}}
	cat.Rows = pciRows(false)
	cat.Count = len(cat.Rows)
	return cat
}

func gpuC() Category {
	cat := Category{Category: "Video adapters", Columns: []string{"Address", "Description", "Vendor", "Device", "Driver"}}
	cat.Rows = pciRows(true)
	cat.Count = len(cat.Rows)
	return cat
}

// ---------- Monitors ----------

func edidName(data []byte) string {
	if len(data) < 128 {
		return ""
	}
	for off := 0x36; off <= 0x7e; off += 0x18 {
		if off+18 > len(data) {
			break
		}
		if data[off+3] == 0xFC {
			raw := strings.Trim(string(data[off+5:off+19]), " \x00\n")
			return strings.TrimSpace(raw)
		}
	}
	return ""
}

func bestMode(s string) string {
	best, area := "", 0
	for _, line := range strings.Fields(s) {
		line = strings.TrimSuffix(line, "*")
		p := strings.SplitN(line, "x", 2)
		if len(p) != 2 {
			continue
		}
		w, e1 := strconv.Atoi(p[0])
		h, e2 := strconv.Atoi(p[1])
		if e1 != nil || e2 != nil {
			continue
		}
		if w*h > area {
			area, best = w*h, line
		}
	}
	return best
}

func monitorC() Category {
	cat := Category{Category: "Monitors", Columns: []string{"Connector", "Status", "Resolution", "Name"}}
	dirs, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		cat.Error = "cannot read /sys/class/drm: " + err.Error()
		return cat
	}
	var rows [][]string
	for _, d := range dirs {
		n := d.Name()
		if !strings.Contains(n, "-") {
			continue // card0, version, etc.
		}
		base := "/sys/class/drm/" + n + "/"
		status := fileGet(base + "status")
		rows = append(rows, []string{n, status, bestMode(fileGet(base + "modes")), edidName(readBytes(base + "edid"))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	if len(rows) == 0 {
		cat.Columns = []string{"Info"}
		cat.Rows = [][]string{{"No display connectors found"}}
		return cat
	}
	cat.Rows, cat.Count = rows, len(rows)
	return cat
}

// ---------- Disks ----------

func isVirtualBlock(n string) bool {
	for _, p := range []string{"loop", "ram", "dm-", "zram", "md", "fd"} {
		if strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}

func diskC() Category {
	cat := Category{Category: "Storage drives", Columns: []string{"Name", "Model", "Size", "Type", "Interface", "Read-only"}}
	dirs, err := os.ReadDir("/sys/block")
	if err != nil {
		cat.Error = "cannot read /sys/block: " + err.Error()
		return cat
	}
	var rows [][]string
	for _, d := range dirs {
		n := d.Name()
		if isVirtualBlock(n) {
			continue
		}
		base := "/sys/block/" + n + "/"
		var sizeB float64
		if s, err := strconv.ParseInt(fileGet(base+"size"), 10, 64); err == nil {
			sizeB = float64(s * 512)
		}
		model := fileGet(base + "device/model")
		if model == "" {
			model = fileGet(base + "device/subsystem_vendor") + " " + fileGet(base+"device/subsystem_device")
		}
		typ := "disk"
		if strings.HasPrefix(n, "sr") {
			typ = "optical"
		}
		iface := "SATA/SCSI"
		switch {
		case strings.HasPrefix(n, "nvme"):
			iface = "NVMe"
		case strings.HasPrefix(n, "mmcblk"):
			iface = "eMMC/SD"
		}
		rows = append(rows, []string{n, strings.TrimSpace(model), humanSize(sizeB), typ, iface, fileGet(base + "ro")})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	if len(rows) == 0 {
		cat.Columns = []string{"Info"}
		cat.Rows = [][]string{{"No block devices found"}}
		return cat
	}
	cat.Rows, cat.Count = rows, len(rows)
	return cat
}

// ---------- CPU ----------

func cpuC() Category {
	cat := Category{Category: "Processor", Columns: []string{"Model", "Logical CPUs", "Physical cores"}}
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		cat.Error = err.Error()
		return cat
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	model, logical := "", 0
	coreSet := map[string]bool{}

	block := map[string]string{}
	flush := func() {
		if len(block) == 0 {
			return
		}
		logical++
		pid, cid := block["physical id"], block["core id"]
		if pid == "" {
			pid = "0"
		}
		if cid == "" {
			cid = "0"
		}
		coreSet[pid+"-"+cid] = true
		block = map[string]string{}
	}

	for sc.Scan() {
		line := sc.Text()
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		p := strings.SplitN(line, ":", 2)
		if len(p) != 2 {
			continue
		}
		key := strings.TrimSpace(p[0])
		val := strings.TrimSpace(p[1])
		if key == "model name" && model == "" {
			model = val
		}
		block[key] = val
	}
	flush()

	if model == "" {
		model = "unknown"
	}
	cat.Rows = [][]string{{model, strconv.Itoa(logical), strconv.Itoa(len(coreSet))}}
	cat.Count = 1
	return cat
}

// ---------- RAM ----------

func ramC() Category {
	cat := Category{Category: "Memory", Columns: []string{"Total", "Used", "Free", "Used %", "Swap total", "Swap free"}}
	vals := map[string]float64{}
	sc, err := os.Open("/proc/meminfo")
	if err != nil {
		cat.Error = err.Error()
		return cat
	}
	defer sc.Close()
	s := bufio.NewScanner(sc)
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) >= 2 {
			if v, err := strconv.ParseFloat(f[1], 64); err == nil {
				vals[f[0][:len(f[0])-1]] = v
			}
		}
	}
	total := vals["MemTotal"]
	avail := vals["MemAvailable"]
	if avail == 0 {
		avail = vals["MemFree"]
	}
	used := total - avail
	pct := 0
	if total > 0 {
		pct = int(used / total * 100)
	}
	cat.Rows = [][]string{
		{
			humanSize(total * 1024),
			humanSize(used * 1024),
			humanSize(avail * 1024),
			fmt.Sprintf("%d%%", pct),
			humanSize(vals["SwapTotal"] * 1024),
			humanSize(vals["SwapFree"] * 1024),
		},
	}
	cat.Count = 1
	return cat
}

// ---------- Battery ----------

func batteryC() Category {
	cat := Category{Category: "Battery", Columns: []string{"Device", "Status", "Capacity %", "Charge", "Voltage mV"}}
	dirs, err := os.ReadDir("/sys/class/power_supply")
	if err != nil {
		cat.Error = "cannot read /sys/class/power_supply: " + err.Error()
		return cat
	}
	var rows [][]string
	for _, d := range dirs {
		base := "/sys/class/power_supply/" + d.Name() + "/"
		if fileGet(base+"type") != "Battery" {
			continue
		}
		charge := ""
		if now, err := strconv.ParseFloat(fileGet(base+"energy_now"), 64); err == nil {
			if full, err2 := strconv.ParseFloat(fileGet(base+"energy_full"), 64); err2 == nil && full > 0 {
				charge = fmt.Sprintf("%.1f Wh (%.0f%%)", now/1e6, now/full*100)
			}
		}
		rows = append(rows, []string{d.Name(), fileGet(base + "status"), fileGet(base + "capacity"), charge, fileGet(base + "voltage_now")})
	}
	if len(rows) == 0 {
		cat.Columns = []string{"Info"}
		cat.Rows = [][]string{{"No battery detected"}}
		return cat
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	cat.Rows, cat.Count = rows, len(rows)
	return cat
}

// ---------- OS ----------

func osC() Category {
	cat := Category{Category: "System", Columns: []string{"Key", "Value"}}
	add := func(k, v string) {
		cat.Rows = append(cat.Rows, []string{k, v})
	}
	for _, line := range strings.Split(fileGet("/etc/os-release"), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			add("OS", strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), "\""))
		}
	}
	if host, err := os.Hostname(); err == nil {
		add("Hostname", host)
	}
	add("Kernel", fileGet("/proc/sys/kernel/osrelease"))
	add("Platform", fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH))
	cat.Count = len(cat.Rows)
	return cat
}