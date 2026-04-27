// Package sysstats samples whole-machine CPU + memory usage.
//
// macOS-only today: shells out to `top -l 1 -n 0 -s 0` (one snapshot, no
// process list, zero settling delay) and parses the summary header. ~50ms
// per call on an M-series Mac, so a 3-5s tick is plenty cheap.
package sysstats

import (
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
)

type Stats struct {
	CPUPct float64 // 0-100, whole-machine (user + sys)
	MemPct float64 // 0-100, used / (used + unused)
}

var (
	cpuRE = regexp.MustCompile(`CPU usage:\s+([\d.]+)%\s+user,\s+([\d.]+)%\s+sys,\s+([\d.]+)%\s+idle`)
	memRE = regexp.MustCompile(`PhysMem:\s+([\d.]+)([KMGTB])\s+used.*?,\s+([\d.]+)([KMGTB])\s+unused`)
)

// Sample reads one CPU/RAM snapshot. Returns zero-valued Stats with no
// error on unsupported platforms — callers can render an empty widget
// without special-casing.
func Sample() (Stats, error) {
	if runtime.GOOS != "darwin" {
		return Stats{}, nil
	}
	out, err := exec.Command("top", "-l", "1", "-n", "0", "-s", "0").Output()
	if err != nil {
		return Stats{}, err
	}
	return parse(string(out)), nil
}

func parse(s string) Stats {
	var st Stats
	if m := cpuRE.FindStringSubmatch(s); len(m) == 4 {
		// Idle is the most stable signal — derive busy as 100 - idle so
		// "user + sys + idle = 100%" rounding errors don't show up.
		idle, _ := strconv.ParseFloat(m[3], 64)
		st.CPUPct = 100 - idle
	}
	if m := memRE.FindStringSubmatch(s); len(m) == 5 {
		used, _ := strconv.ParseFloat(m[1], 64)
		used *= unitMul(m[2])
		unused, _ := strconv.ParseFloat(m[3], 64)
		unused *= unitMul(m[4])
		total := used + unused
		if total > 0 {
			st.MemPct = used / total * 100
		}
	}
	return st
}

func unitMul(u string) float64 {
	switch u {
	case "K":
		return 1024
	case "M":
		return 1024 * 1024
	case "G":
		return 1024 * 1024 * 1024
	case "T":
		return 1024 * 1024 * 1024 * 1024
	}
	return 1
}
