# UVCD — Utility for Viewing Connected Devices

A cross-platform tool that scans the devices connected to your computer and
shows them as live, color-coded tables.

Two components, one workflow:

- **`scanner/`** — a Go program for **Windows** and **Linux**. It enumerates USB,
  PCI, GPU, monitors, network interfaces, storage, CPU, RAM, battery and OS
  info, and prints the result as **JSON**. It works in two ways: as a one-shot
  CLI and as an interactive stdin/stdout service (one JSON line per command).
- **`src/main.py`** — a Python UI built on [Rich](https://github.com/Textualize/rich).
  It starts the Go binary through `subprocess`, forwards the user's commands,
  reads the JSON and renders it as dynamic, color-coded tables.

You can run the Python UI as a **standalone interactive program**, or call it
**from a terminal / scripts** for one-shot scans.

---

## Features

| Category   | Description                                  |
| ---------- | -------------------------------------------- |
| `usb`      | USB devices (name, manufacturer, status)     |
| `pci`      | PCI / expansion devices                      |
| `gpu`      | video adapters                               |
| `monitor`  | connected monitors                           |
| `net`      | network interfaces (MAC, IP, state)          |
| `disk`     | storage drives                               |
| `cpu`      | processor info                               |
| `ram`      | memory usage                                 |
| `battery`  | battery status                               |
| `os`       | operating system / host info                 |
| `all`      | every category at once                       |

---

## Requirements

- **Go** 1.22+ (to build the scanner, only needed for compilation)
- **Python** 3.9+ with `rich` (`pip install rich`)
- **Windows**: PowerShell 5.1+ (already present)
- **Linux**: standard tools (`lsblk`, `/sys`, `/proc`)

---

## Build

### Windows

```bat
build.bat
```

produces `bin\scan-devices.exe` and `bin\scan-devices-linux`.

### Linux

```sh
./build.sh
```

produces `bin/scan-devices`.

Or build manually:

```sh
# Linux
CGO_ENABLED=0 go build -o bin/scan-devices ./scanner

# Windows (from Linux/macOS)
CGO_ENABLED=0 GOOS=windows go build -o bin/scan-devices.exe ./scanner
```

---

## Usage

### Standalone interactive program

```sh
python src/main.py
```

Opens a Rich REPL. Type a category name (`usb`, `net`, ...), `all`,
`watch 3` (auto-refresh), `help`, or `exit`. Press **Enter** for `all`.

### From a terminal (one-shot)

```sh
# print tables for one category and exit
python src/main.py usb

# scan every category
python src/main.py all

# dump raw JSON (great for piping into jq)
python src/main.py ram --json

# auto-refresh mode
python src/main.py --watch 2

# command list
python src/main.py --help
```

### Using the Go scanner directly

The scanner alone is also usable from the terminal:

```sh
# one-shot: print JSON and exit
./scan-devices usb
./scan-devices all
./scan-devices --version

# interactive JSON service (used by the Python UI)
./scan-devices
> usb
{"status":"ok","command":"usb","categories":[...]}
> exit
{"status":"bye","command":"exit"}
```

---

## JSON protocol

Every command produces exactly one JSON object per line:

```json
{
  "status": "ok",
  "command": "usb",
  "categories": [
    {
      "category": "USB devices",
      "count": 8,
      "columns": ["Name", "Manufacturer", "Status", "Device ID"],
      "rows": [
        ["Realtek Bluetooth 4.2 Adapter", "Realtek Semiconductor Corp.", "OK", "USB\\VID_0BDA&PID_B00E\\00E04C000001"]
      ]
    }
  ],
  "text": "8 usb devices"
}
```

Statuses: `ok`, `error`, `help`, `bye`. The Python UI turns `columns` + `rows`
into a Rich table; unknown status values are automatically color-coded.

---

## Project layout

```
.
├── scanner/                 # Go backend
│   ├── main.go              # CLI + interactive JSON service
│   ├── devices_common.go    # category dispatch, network
│   ├── devices_windows.go   # Windows implementation (PowerShell/WMI)
│   ├── devices_linux.go     # Linux implementation (sysfs/proc)
│   ├── devices_other.go     # fallback for unsupported platforms
│   └── main_test.go         # unit tests
├── src/
│   └── main.py              # Rich UI (standalone + one-shot)
├── bin/                     # compiled binaries (gitignored)
├── .github/workflows/ci.yml # GitHub Actions CI/CD
├── build.bat                # Windows build script
├── build.sh                 # Linux build script
├── go.mod
└── README.md
```

---

## CI / CD

`.github/workflows/ci.yml` runs on every push and pull request:

- `go vet` and `go test` on Linux and Windows
- Python import/compile checks and `--help` smoke test on both OSes
- an end-to-end Windows job that scans real hardware and validates JSON
- on `v*` tags, builds `scan-devices-linux` and `scan-devices-windows-amd64.exe`
  and attaches them to a GitHub Release.

---

## License

MIT — see [LICENSE](LICENSE).