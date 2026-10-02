//go:build !windows && !linux

package main

import "runtime"

func unsupported(name string) Category {
	return Category{Category: name, Columns: []string{"Info"}, Error: "not supported on " + runtime.GOOS}
}

func usbC() Category     { return unsupported("USB devices") }
func pciC() Category     { return unsupported("PCI devices") }
func gpuC() Category     { return unsupported("Video adapters") }
func monitorC() Category { return unsupported("Monitors") }
func diskC() Category    { return unsupported("Storage drives") }
func cpuC() Category     { return unsupported("Processor") }
func ramC() Category     { return unsupported("Memory") }
func batteryC() Category { return unsupported("Battery") }
func osC() Category      { return unsupported("System") }
