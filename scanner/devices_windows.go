//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pnpItem struct {
	C string `json:"C"`
	N string `json:"N"`
	M string `json:"M"`
	S string `json:"S"`
	I string `json:"I"`
}

type diskItem struct {
	N string `json:"N"`
	S string `json:"S"`
	T string `json:"T"`
	M string `json:"M"`
	P string `json:"P"`
	I string `json:"I"`
}

type vidItem struct {
	N string `json:"N"`
	R string `json:"R"`
	D string `json:"D"`
	S string `json:"S"`
	I string `json:"I"`
}

type monItem struct {
	N string `json:"N"`
	W string `json:"W"`
	H string `json:"H"`
	S string `json:"S"`
	M string `json:"M"`
	I string `json:"I"`
}

type cpuItem struct {
	N string `json:"N"`
	C string `json:"C"`
	L string `json:"L"`
	S string `json:"S"`
}

type memItem struct {
	T string `json:"T"`
	F string `json:"F"`
}

type batItem struct {
	N string `json:"N"`
	S string `json:"S"`
	C string `json:"C"`
	Y string `json:"Y"`
	I string `json:"I"`
}

type osItem struct {
	K string `json:"K"`
	V string `json:"V"`
}

const winScript = `
$ErrorActionPreference='SilentlyContinue'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)

$usb = @(Get-CimInstance Win32_PnPEntity | Where-Object { $_.Name -and ($_.PNPClass -eq 'USB' -or $_.DeviceID -like 'USB\*') } | ForEach-Object { [pscustomobject]@{ C=$_.PNPClass; N=$_.Name; M=$_.Manufacturer; S=$_.Status; I=$_.DeviceID } })
$pci = @(Get-CimInstance Win32_PnPEntity | Where-Object { $_.Name -and $_.DeviceID -like 'PCI\*' } | ForEach-Object { [pscustomobject]@{ C=$_.PNPClass; N=$_.Name; M=$_.Manufacturer; S=$_.Status; I=$_.DeviceID } })
$disk = @(Get-CimInstance Win32_DiskDrive | ForEach-Object { [pscustomobject]@{ N=$_.Model; S=[string]$_.Size; T=$_.InterfaceType; M=$_.MediaType; P=$_.Status; I=$_.DeviceID } })
$vid = @(Get-CimInstance Win32_VideoController | ForEach-Object { [pscustomobject]@{ N=$_.Name; R=[string]$_.AdapterRAM; D=$_.DriverVersion; S=$_.Status; I=$_.PNPDeviceID } })
$mon = @(Get-CimInstance Win32_DesktopMonitor | ForEach-Object { [pscustomobject]@{ N=$_.Name; W=[string]$_.ScreenWidth; H=[string]$_.ScreenHeight; S=$_.Status; M=$_.Manufacturer; I=$_.PNPDeviceID } })
$cpu = @(Get-CimInstance Win32_Processor | ForEach-Object { [pscustomobject]@{ N=$_.Name; C=[string]$_.NumberOfCores; L=[string]$_.NumberOfLogicalProcessors; S=$_.SocketDesignation } })
$os = (Get-CimInstance Win32_OperatingSystem | Select-Object -First 1)
if (-not $os) { $os = [pscustomobject]@{ Caption='unknown'; Version=''; OSArchitecture='' } }
$cs = (Get-CimInstance Win32_ComputerSystem | Select-Object -First 1)
$tot='0'; if ($cs) { $tot=[string]$cs.TotalPhysicalMemory }
$free='0'; if ($os) { $free=[string]($os.FreePhysicalMemory*1024) }
$mem = [pscustomobject]@{ T=$tot; F=$free }
$bat = @(Get-CimInstance Win32_Battery | ForEach-Object { [pscustomobject]@{ N=$_.Name; S=[string]$_.BatteryStatus; C=[string]$_.EstimatedChargeRemaining; Y=$_.Chemistry; I=$_.DeviceID } })
$osarr = @(
  [pscustomobject]@{ K='OS';             V=$os.Caption }
  [pscustomobject]@{ K='Version';        V=$os.Version }
  [pscustomobject]@{ K='Architecture';   V=$os.OSArchitecture }
  [pscustomobject]@{ K='Hostname';       V=$env:COMPUTERNAME }
  [pscustomobject]@{ K='Platform';       V=$env:OS }
)
[pscustomobject]@{ usb=$usb; pci=$pci; disk=$disk; vid=$vid; mon=$mon; cpu=$cpu; mem=$mem; bat=$bat; os=$osarr } | ConvertTo-Json -Compress -Depth 6
`

var (
	psMu    sync.Mutex
	psDocC  map[string]json.RawMessage
	psDocAt time.Time
	psErr   error
)

// psDoc runs PowerShell once and caches the parsed result for one second.
//
// The cache timestamp is set ONLY after a successful parse. If it were set
// before launching PowerShell, a slow first run would immediately invalidate
// the cache and force "all" to relaunch PowerShell for every single category.
func psDoc() (map[string]json.RawMessage, error) {
	psMu.Lock()
	defer psMu.Unlock()
	if psErr == nil && psDocC != nil && time.Since(psDocAt) < time.Second {
		return psDocC, nil
	}

	out := runPS(winScript)
	if out == nil {
		psErr = errors.New("powershell returned no output")
		psDocC = nil
		return nil, psErr
	}
	clean := []byte(strings.ToValidUTF8(string(out), "\uFFFD"))
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(clean, &doc); err != nil {
		psErr = fmt.Errorf("cannot parse powershell output: %v", err)
		psDocC = nil
		return nil, psErr
	}
	psErr, psDocC = nil, doc
	psDocAt = time.Now()
	return doc, nil
}

func eachT[T any](doc map[string]json.RawMessage, key string, fn func(T)) {
	raw, ok := doc[key]
	if !ok {
		return
	}
	var arr []T
	if err := json.Unmarshal([]byte(raw), &arr); err == nil {
		for _, it := range arr {
			fn(it)
		}
		return
	}
	var one T
	if err := json.Unmarshal([]byte(raw), &one); err == nil {
		fn(one)
	}
}

// runPS launches PowerShell without a shell and with WaitDelay enabled.
//
// exec.CommandContext(...).Output() is a known trap on Windows: the context
// only kills powershell.exe itself, while its children (WMI/CIM) keep the
// inherited stdout/stderr handles open. Output() then waits for EOF forever.
// WaitDelay forces Go to close the pipes and return control after a few
// seconds past cancellation.
func runPS(script string) []byte {
	enc := base64.StdEncoding.EncodeToString(utf16le(script))
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "powershell.exe",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-EncodedCommand", enc)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.WaitDelay = 5 * time.Second

	if err := cmd.Run(); err != nil {
		return nil
	}
	return stdout.Bytes()
}

func utf16le(s string) []byte {
	var b []byte
	for _, r := range s {
		if r > 0xFFFF {
			r = 0xFFFD
		}
		b = append(b, byte(r), byte(r>>8))
	}
	return b
}

func winErrCat(name, msg string) Category {
	return Category{Category: name, Columns: []string{"Info"}, Rows: [][]string{{msg}}, Error: msg}
}

func sortRows(rows [][]string) {
	sort.Slice(rows, func(i, j int) bool {
		if len(rows[i]) == 0 {
			return false
		}
		return rows[i][0] < rows[j][0]
	})
}

func usbC() Category {
	cat := Category{Category: "USB devices", Columns: []string{"Name", "Manufacturer", "Status", "Device ID"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("USB devices", err.Error())
	}
	eachT(doc, "usb", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), strv(m, "M"), strv(m, "S"), strv(m, "I")})
	})
	sortRows(cat.Rows)
	cat.Count = len(cat.Rows)
	return cat
}

func pciC() Category {
	cat := Category{Category: "PCI devices", Columns: []string{"Name", "Class", "Manufacturer", "Status", "Device ID"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("PCI devices", err.Error())
	}
	eachT(doc, "pci", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), strv(m, "C"), strv(m, "M"), strv(m, "S"), strv(m, "I")})
	})
	sortRows(cat.Rows)
	cat.Count = len(cat.Rows)
	return cat
}

func gpuC() Category {
	cat := Category{Category: "Video adapters", Columns: []string{"Name", "Video RAM", "Driver", "Status"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Video adapters", err.Error())
	}
	eachT(doc, "vid", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), humanSize(parseFloat(strv(m, "R"))), strv(m, "D"), strv(m, "S")})
	})
	sortRows(cat.Rows)
	cat.Count = len(cat.Rows)
	return cat
}

func monitorC() Category {
	cat := Category{Category: "Monitors", Columns: []string{"Name", "Resolution", "Manufacturer", "Status"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Monitors", err.Error())
	}
	eachT(doc, "mon", func(m map[string]any) {
		w, h := strv(m, "W"), strv(m, "H")
		res := "-"
		if w != "" && h != "" && w != "0" && h != "0" {
			res = w + "x" + h
		}
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), res, strv(m, "M"), strv(m, "S")})
	})
	sortRows(cat.Rows)
	cat.Count = len(cat.Rows)
	return cat
}

func diskC() Category {
	cat := Category{Category: "Storage drives", Columns: []string{"Model", "Size", "Interface", "Media", "Status"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Storage drives", err.Error())
	}
	eachT(doc, "disk", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), humanSize(parseFloat(strv(m, "S"))), strv(m, "T"), strv(m, "M"), strv(m, "P")})
	})
	sortRows(cat.Rows)
	cat.Count = len(cat.Rows)
	return cat
}

func cpuC() Category {
	cat := Category{Category: "Processor", Columns: []string{"Model", "Physical cores", "Logical processors", "Socket"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Processor", err.Error())
	}
	eachT(doc, "cpu", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strings.TrimSpace(strv(m, "N")), strv(m, "C"), strv(m, "L"), strv(m, "S")})
	})
	cat.Count = len(cat.Rows)
	return cat
}

func ramC() Category {
	cat := Category{Category: "Memory", Columns: []string{"Total", "Used", "Free", "Used %"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Memory", err.Error())
	}
	var tot, free float64
	eachT(doc, "mem", func(m map[string]any) {
		tot = parseFloat(strv(m, "T"))
		free = parseFloat(strv(m, "F"))
	})
	used := tot - free
	pct := 0
	if tot > 0 {
		pct = int(used / tot * 100)
	}
	cat.Rows = [][]string{{humanSize(tot), humanSize(used), humanSize(free), fmt.Sprintf("%d%%", pct)}}
	cat.Count = 1
	return cat
}

func batteryName(code string) string {
	switch code {
	case "1":
		return "Discharging"
	case "2":
		return "On AC power"
	case "3":
		return "Fully charged"
	case "4":
		return "Low"
	case "5":
		return "Critical"
	case "6", "7", "8", "9":
		return "Charging"
	case "10":
		return "Unknown"
	case "11":
		return "Partially charged"
	}
	return code
}

func batteryC() Category {
	cat := Category{Category: "Battery", Columns: []string{"Name", "Status", "Capacity %", "Chemistry"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("Battery", err.Error())
	}
	eachT(doc, "bat", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "N"), batteryName(strv(m, "S")), strv(m, "C"), strv(m, "Y")})
	})
	if len(cat.Rows) == 0 {
		cat.Columns = []string{"Info"}
		cat.Rows = [][]string{{"No battery detected"}}
	}
	cat.Count = len(cat.Rows)
	return cat
}

func osC() Category {
	cat := Category{Category: "System", Columns: []string{"Key", "Value"}}
	doc, err := psDoc()
	if err != nil {
		return winErrCat("System", err.Error())
	}
	eachT(doc, "os", func(m map[string]any) {
		cat.Rows = append(cat.Rows, []string{strv(m, "K"), strv(m, "V")})
	})
	cat.Count = len(cat.Rows)
	return cat
}

func strv(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
