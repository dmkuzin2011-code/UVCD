#!/usr/bin/env python3
"""UVCD - Rich terminal UI for the Go device scanner.

The Python UI launches the compiled Go binary (bin/scan-devices[.exe]) through
subprocess, forwards the user's commands to it on stdin, reads the JSON
responses from stdout, and renders every category as a dynamic Rich table.

Usage:
    python main.py                interactive menu (default)
    python main.py --menu         same as above
    python main.py usb            scan one category
    python main.py all --json     dump raw scanner JSON
    python main.py --watch 2      auto-refresh every 2 seconds
    python main.py --help         show the command list
"""

import argparse
import json
import os
import subprocess
import sys
import threading
import time

from rich import box
from rich.align import Align
from rich.console import Console, Group
from rich.live import Live
from rich.panel import Panel
from rich.prompt import Prompt
from rich.table import Table
from rich.text import Text

try:
    import questionary
    from questionary import Style as QStyle
    HAVE_QUESTIONARY = True
except ImportError:
    HAVE_QUESTIONARY = False


ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
_HERE = os.path.dirname(os.path.abspath(__file__))

COMMANDS = [
    ("all", "scan every category"),
    ("usb", "USB devices"),
    ("pci", "PCI devices"),
    ("gpu", "video adapters"),
    ("monitor", "connected monitors"),
    ("net", "network interfaces"),
    ("disk", "storage drives"),
    ("cpu", "processor info"),
    ("ram", "memory usage"),
    ("battery", "battery status"),
    ("os", "system info"),
    ("watch [N]", "auto-refresh every N seconds"),
    ("help", "show commands"),
    ("exit", "quit"),
]

MENU_CHOICES = [
    ("all", "Scan everything"),
    ("usb", "USB devices"),
    ("pci", "PCI devices"),
    ("gpu", "Video adapters"),
    ("monitor", "Monitors"),
    ("net", "Network interfaces"),
    ("disk", "Storage drives"),
    ("cpu", "Processor"),
    ("ram", "Memory"),
    ("battery", "Battery"),
    ("os", "System info"),
    ("watch", "Auto-refresh (live view)"),
    ("help", "Help"),
    ("exit", "Exit"),
]


def _binary_candidates():
    """Return the list of paths where the scanner binary may live."""
    names = []
    if sys.platform.startswith("win"):
        names.append("scan-devices.exe")
    else:
        names.append("scan-devices")
        names.append("scan-devices-linux")

    bases = [
        os.path.join(ROOT, "bin"),
        ROOT,
        _HERE,
    ]

    candidates = []
    for base in bases:
        for name in names:
            candidates.append(os.path.join(base, name))
    return candidates


def pick_binary():
    """Find the right Go binary for the current operating system."""
    for c in _binary_candidates():
        if os.path.isfile(c):
            return c
    return _binary_candidates()[0]


def _drain_stderr(pipe):
    """Read stderr in the background so the pipe never fills up."""
    try:
        for line in iter(pipe.readline, ""):
            if not line:
                break
            # Uncomment for debugging:
            # sys.stderr.write("[scanner] " + line)
    except Exception:
        pass


def start_process(binary):
    """Spawn the Go scanner as a persistent subprocess with pipes."""
    flags = subprocess.CREATE_NO_WINDOW if os.name == "nt" else 0
    proc = subprocess.Popen(
        [binary],
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        encoding="utf-8",
        errors="replace",
        bufsize=1,
        creationflags=flags,
    )
    threading.Thread(target=_drain_stderr, args=(proc.stderr,), daemon=True).start()
    return proc


def send(proc, command):
    proc.stdin.write(command + "\n")
    proc.stdin.flush()


def recv(proc):
    """Read one JSON line from the Go process. Returns a dict or None."""
    line = proc.stdout.readline()
    if not line:
        return None
    try:
        return json.loads(line)
    except json.JSONDecodeError:
        return {"status": "error", "raw": line.strip()}


# --------------------------------------------------------------------------
# Markup / styling helpers
# --------------------------------------------------------------------------

OK_VALUES = {
    "ok", "up", "operational", "present", "connected", "enabled", "good",
    "full", "fully charged", "charging",
}
BAD_VALUES = {"error", "down", "degraded", "disconnected", "critical", "low", "offline"}
WARN_VALUES = {"unknown", "degraded!", "unavailable", "partially charged", "discharging"}


def status_style(value):
    s = str(value or "").strip().lower()
    if s in BAD_VALUES:
        return "red"
    if s in WARN_VALUES:
        return "yellow"
    if s in OK_VALUES:
        return "green"
    return "cyan" if s else "dim"


def cell_style(column, value):
    if value is None or value == "":
        return "dim"
    col = (column or "").strip().lower()
    if any(k in col for k in ("status", "state")) and "destination" not in col:
        return status_style(value)
    if any(k in col for k in ("count", "total", "used", "capacity", "logical", "core")):
        return "bold magenta"
    if any(k in col for k in ("mac", "id", "serial", "ip", "address", "driver")):
        return "cyan"
    if any(k in col for k in ("name", "model", "vendor", "manufacturer")):
        return "bold white"
    if any(k in col for k in ("type", "class", "interface", "media")):
        return "magenta"
    return "white"


def category_table(category):
    title = category.get("category") or "Devices"
    columns = category.get("columns") or ["Value"]
    table = Table(
        title=title,
        box=box.ROUNDED,
        header_style="bold cyan",
        title_style="bold magenta",
        expand=True,
        collapse_padding=True,
    )
    table.add_column("#", style="dim", no_wrap=True)
    for col in columns:
        table.add_column(col, overflow="fold", no_wrap=False, min_width=4)

    rows = category.get("rows") or []
    for i, row in enumerate(rows, 1):
        if isinstance(row, str):
            row = [row]
        row = list(row)
        while len(row) < len(columns):
            row.append("")
        cells = []
        for col, value in zip(columns, row[: len(columns)]):
            text = str(value) if value != "" and value is not None else "-"
            cells.append(Text(text, style=cell_style(col, value)))
        table.add_row(str(i), *cells)
    table.caption = Text(
        f"count: {category.get('count', len(rows))}", style="bold magenta"
    )
    return table


def render_response(response, console):
    if not response:
        console.print("[red]Scanner closed its output (empty response).[/]")
        return
    status = response.get("status")
    if status == "bye":
        return
    if status == "help":
        console.print(
            Panel(str(response.get("text", "")), title="Help", border_style="cyan")
        )
        return
    if status == "error":
        console.print(
            Panel(
                str(response.get("text") or response.get("raw") or "unknown error"),
                title="Error",
                border_style="red",
            )
        )
        return

    categories = response.get("categories") or []
    if not categories:
        console.print("[dim]No data.[/]")
        return
    console.print(Group(*(category_table(c) for c in categories)))
    summary = response.get("text")
    if summary:
        console.print(Text("  " + str(summary), style="bold magenta"))
    console.print()


def print_help(console):
    lines = "\n".join(f"  {name:<10} {desc}" for name, desc in COMMANDS)
    console.print(Panel(lines, title="Commands", border_style="cyan"))


def canonical(command):
    lower = command.split()[0].lower() if command else ""
    if lower in ("all", "list", "ls", "scan", "refresh", ""):
        return "all"
    if lower in ("net", "network", "networks"):
        return "net"
    if lower in ("disk", "disks", "storage", "block"):
        return "disk"
    if lower in ("os", "system", "info"):
        return "os"
    if lower in ("ram", "mem", "memory"):
        return "ram"
    if lower in ("bat", "battery", "power"):
        return "battery"
    if lower in ("vid", "video", "gpu", "gfx"):
        return "gpu"
    if lower in ("mon", "monitor", "monitors", "display"):
        return "monitor"
    if lower in ("cpu", "processor"):
        return "cpu"
    if lower in ("usb", "pci"):
        return lower
    return None


# --------------------------------------------------------------------------
# Modes
# --------------------------------------------------------------------------

def run_json(proc, console, command):
    send(proc, command)
    response = recv(proc)
    if response:
        console.print(json.dumps(response, ensure_ascii=False))


def watch_mode(proc, console, interval):
    try:
        with Live(console=console, screen=True, auto_refresh=True) as live:
            while True:
                send(proc, "all")
                response = recv(proc)
                if not response or response.get("status") != "ok":
                    live.update(
                        Panel("[red]Scanner did not respond.[/]", border_style="red")
                    )
                    return
                tables = [category_table(c) for c in (response.get("categories") or [])]
                summary = Text(
                    str(response.get("text", "")),
                    style="bold magenta",
                    justify="center",
                )
                group = Group(
                    Align.center(
                        Text("UVCD - press Ctrl+C to stop", style="italic cyan")
                    ),
                    Align.center(summary),
                    Group(*tables),
                )
                live.update(Panel(group, border_style="blue"))
                time.sleep(interval)
    except KeyboardInterrupt:
        console.print("\n[yellow]Watch mode stopped.[/]")


def _ask_menu(console):
    """Ask the user to pick a category. Returns a command string or None."""
    if HAVE_QUESTIONARY:
        style = QStyle(
            [
                ("qmark", "fg:#00afff bold"),
                ("question", "bold"),
                ("answer", "fg:#00afff bold"),
                ("pointer", "fg:#00afff bold"),
                ("highlighted", "fg:#00afff bold"),
                ("selected", "fg:#00ff87"),
            ]
        )
        choices = [
            questionary.Choice(title=f"{name:<10} - {desc}", value=name)
            for name, desc in MENU_CHOICES
        ]
        answer = questionary.select(
            "What do you want to scan?",
            choices=choices,
            style=style,
            use_shortcuts=False,
        ).ask()
        return answer

    # Fallback when questionary is not installed.
    console.print("[yellow]questionary not installed, falling back to text input.[/]")
    console.print(
        "[cyan]Available: all, usb, pci, gpu, monitor, net, disk, cpu, ram, battery, os, watch, help, exit[/]"
    )
    try:
        return Prompt.ask("[bold cyan]Command[/]", default="all").strip()
    except (KeyboardInterrupt, EOFError):
        return "exit"


def menu_loop(proc, console):
    """Arrow-key menu driven REPL. Returns when the user picks Exit."""
    while True:
        cmd = _ask_menu(console)
        if cmd is None or cmd in ("exit", "quit", "q", "bye"):
            return
        if cmd == "help":
            print_help(console)
            continue
        if cmd == "watch":
            try:
                interval = float(
                    Prompt.ask("[bold cyan]Interval (seconds)[/]", default="3")
                )
                interval = max(0.5, interval)
            except (ValueError, KeyboardInterrupt, EOFError):
                interval = 3.0
            watch_mode(proc, console, interval)
            continue

        handle = canonical(cmd)
        if handle is None:
            console.print(f"[red]Unknown command:[/] {cmd}")
            continue
        send(proc, handle)
        render_response(recv(proc), console)
        try:
            Prompt.ask("[dim]Press Enter to continue[/]", default="")
        except (KeyboardInterrupt, EOFError):
            return


def interactive_mode(proc, console):
    """Interactive mode: show the menu first, scan only on user request."""
    console.print(r"""[red]
    •   • •   •  •••  ••••  
    •   • •   • •     •   • 
    •   • •   • •     •   • 
    •   •  • •  •     •   • 
     •••    •    •••  ••••  
    [red]""")
    console.print(
        Panel.fit(
            "[bold magenta]UVCD[/] - live view of devices connected to this computer.\n"
            "[cyan]Pick a category from the menu below.\n"
            "Use [bold]\u2191[/]/[bold]\u2193[/] to move, [bold]Enter[/] to select, "
            "[bold]Exit[/] to quit.",
            border_style="red",
        )
    )
    menu_loop(proc, console)
    console.print("[green]Bye.[/]")


# --------------------------------------------------------------------------
# Entry point
# --------------------------------------------------------------------------

def build_parser():
    parser = argparse.ArgumentParser(
        prog="uvcd",
        description="UVCD - Rich terminal UI for the Go device scanner.",
        add_help=False,
    )
    parser.add_argument(
        "command",
        nargs="?",
        default=None,
        help="category to scan: all, usb, pci, gpu, monitor, net, disk, cpu, ram, battery, os",
    )
    parser.add_argument("--json", action="store_true", help="print raw scanner JSON")
    parser.add_argument(
        "--watch",
        nargs="?",
        const=3,
        type=float,
        default=0,
        help="auto-refresh mode (interval in seconds)",
    )
    parser.add_argument(
        "--menu",
        action="store_true",
        help="force interactive menu (default when no arguments are given)",
    )
    parser.add_argument("-h", "--help", action="store_true", dest="show_help")
    return parser


def main():
    args = build_parser().parse_known_args()[0]
    console = Console()

    if args.show_help:
        print_help(console)
        sys.exit(0)

    binary = pick_binary()
    if not os.path.isfile(binary):
        console.print(f"[red]Scanner binary not found:[/] {binary}")
        console.print(
            "Build it first with [bold]build.bat[/] (Windows) or "
            "[bold]build.sh[/] (Linux)."
        )
        sys.exit(1)

    try:
        proc = start_process(binary)
    except Exception as exc:  # noqa: BLE001
        console.print(f"[red]Failed to start scanner:[/] {exc}")
        sys.exit(1)

    if args.watch:
        watch_mode(proc, console, args.watch)
        send(proc, "exit")
        proc.terminate()
        return

    if args.command:
        handle = canonical(args.command)
        if handle is None:
            console.print(f"[red]Unknown category:[/] {args.command}")
            console.print("Run with [bold]--help[/] for the list of commands.")
            proc.terminate()
            sys.exit(2)
        if args.json:
            run_json(proc, console, handle)
        else:
            send(proc, handle)
            render_response(recv(proc), console)
        send(proc, "exit")
        try:
            proc.wait(timeout=5)
        except Exception:  # noqa: BLE001
            proc.kill()
        return

    # Default: interactive menu, no automatic scan.
    interactive_mode(proc, console)
    send(proc, "exit")
    try:
        proc.wait(timeout=5)
    except Exception:  # noqa: BLE001
        proc.kill()


if __name__ == "__main__":
    main()