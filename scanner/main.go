package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Version is the scanner release version.
const Version = "1.0.0"

// Category is one table of data: the client turns columns+rows into a Rich table.
type Category struct {
	Category string     `json:"category"`
	Count    int        `json:"count"`
	Columns  []string   `json:"columns"`
	Rows     [][]string `json:"rows"`
	Error    string     `json:"error,omitempty"`
}

// Response is the JSON envelope written to stdout for every command.
type Response struct {
	Status     string     `json:"status"` // ok | error | help | bye
	Command    string     `json:"command"`
	Categories []Category `json:"categories,omitempty"`
	Text       string     `json:"text,omitempty"`
}

// aliases maps every accepted command word to its canonical category.
var aliases = map[string]string{
	"usb":       "usb",
	"pci":       "pci",
	"gpu":       "gpu",
	"video":     "gpu",
	"monitor":   "monitor",
	"monitors":  "monitor",
	"display":   "monitor",
	"net":       "net",
	"network":   "net",
	"disk":      "disk",
	"disks":     "disk",
	"storage":   "disk",
	"block":     "disk",
	"cpu":       "cpu",
	"processor": "cpu",
	"ram":       "ram",
	"memory":    "ram",
	"mem":       "ram",
	"battery":   "battery",
	"batteries": "battery",
	"power":     "battery",
	"os":        "os",
	"system":    "os",
	"info":      "os",
}

func usageText() string {
	cmds := [][2]string{
		{"all", "scan every category (default)"},
		{"usb", "USB devices"},
		{"pci", "PCI / expansion devices"},
		{"gpu", "video adapters"},
		{"monitor", "connected monitors"},
		{"net", "network interfaces"},
		{"disk", "storage drives"},
		{"cpu", "processor info"},
		{"ram", "memory info"},
		{"battery", "battery status"},
		{"os", "operating system / host info"},
		{"help", "this help"},
		{"exit", "quit"},
	}
	var b strings.Builder
	b.WriteString("scan-devices - scan devices connected to this computer\n")
	b.WriteString("\nUsage:\n")
	b.WriteString("  scan-devices [category]         scan one category and print JSON, then exit\n")
	b.WriteString("  scan-devices all                scan every category\n")
	b.WriteString("  scan-devices --version          print version\n")
	b.WriteString("  scan-devices --help             show this help\n")
	b.WriteString("  (no arguments)                  interactive mode: read commands from stdin\n")
	b.WriteString("\nCommands:\n")
	for _, c := range cmds {
		fmt.Fprintf(&b, "  %-10s %s\n", c[0], c[1])
	}
	return b.String()
}

func main() {
	if len(os.Args) > 1 {
		os.Exit(runCLI(strings.Join(os.Args[1:], " ")))
	}
	interactive()
}

// runCLI handles one-shot invocations from a terminal, e.g. `scan-devices usb`.
func runCLI(cmd string) int {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return 0
	}
	base := strings.ToLower(strings.SplitN(cmd, " ", 2)[0])
	switch base {
	case "-h", "--help", "help", "?":
		fmt.Println(usageText())
		return 0
	case "-v", "--version", "version":
		fmt.Printf("scan-devices %s (%s/%s)\n", Version, buildOS, buildArch)
		return 0
	case "exit", "quit", "q", "bye":
		return 0
	}

	var resp Response
	switch base {
	case "all", "everything", "list", "ls", "refresh", "devices":
		cats := allCategories()
		resp = Response{Status: "ok", Command: cmd, Categories: cats, Text: categoriesSummary(cats)}
	default:
		canonical, ok := aliases[base]
		if !ok {
			resp = Response{Status: "error", Command: cmd, Text: "unknown command: " + cmd + "\n" + usageText()}
			writeJSON(resp)
			return 2
		}
		if cat, found := categoryByName(canonical); found {
			resp = Response{Status: "ok", Command: cmd, Categories: []Category{cat}}
		} else {
			resp = Response{Status: "error", Command: cmd, Text: "category unavailable: " + canonical}
		}
	}
	writeJSON(resp)
	if resp.Status == "error" {
		return 2
	}
	return 0
}

// interactive reads commands from stdin and prints one JSON line per command.
func interactive() {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)

	for sc.Scan() {
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		cmd := strings.ToLower(raw)
		base := strings.Fields(cmd)[0]

		var resp Response
		switch base {
		case "help", "?":
			resp = Response{Status: "help", Command: cmd, Text: usageText()}
		case "exit", "quit", "q", "bye":
			writeJSON(Response{Status: "bye", Command: cmd})
			return
		case "all", "everything", "list", "ls", "refresh", "devices":
			cats := allCategories()
			resp = Response{Status: "ok", Command: cmd, Categories: cats, Text: categoriesSummary(cats)}
		default:
			canonical, ok := aliases[base]
			if !ok {
				resp = Response{Status: "error", Command: cmd, Text: "unknown command: " + raw + "\n" + usageText()}
				writeJSON(resp)
				continue
			}
			if cat, found := categoryByName(canonical); found {
				resp = Response{Status: "ok", Command: cmd, Categories: []Category{cat}}
			} else {
				resp = Response{Status: "error", Command: cmd, Text: "category unavailable: " + canonical}
			}
		}
		writeJSON(resp)
	}

	// Surface any read error from stdin instead of silently exiting.
	if err := sc.Err(); err != nil {
		writeJSON(Response{Status: "error", Command: "stdin", Text: err.Error()})
	}
}

func writeJSON(v interface{}) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	fmt.Println(string(b))
}

func categoriesSummary(cats []Category) string {
	var parts []string
	for _, c := range cats {
		if c.Count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.Count, strings.ToLower(c.Category)))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, " | ")
}

func humanSize(bytes float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v, i := bytes, 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}
