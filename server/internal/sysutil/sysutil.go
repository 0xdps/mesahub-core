// Package sysutil provides OS-level helpers: disk usage and path utilities.
package sysutil

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// FileSizeBytes returns the size in bytes of the file at path, or 0 if the
// file does not exist or cannot be stat'd.
func FileSizeBytes(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// DBPath returns the canonical path for a database file.
func DBPath(dataPath, name string) string {
	return filepath.Join(dataPath, name+".db")
}

// VolumeUsagePct returns the current disk usage percentage for the given path
// by running `df -k`. Returns 0 on any error.
func VolumeUsagePct(path string) int {
	out, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return 0
	}
	return parseDFOutput(string(out)).usedPct
}

// VolumeSummary holds the disk usage for a single mount point.
type VolumeSummary struct {
	Used        int64
	Total       int64
	UsedPercent int
}

// VolumeInfo returns used/total bytes and percentage for the given path.
func VolumeInfo(path string) VolumeSummary {
	out, err := exec.Command("df", "-k", path).Output()
	if err != nil {
		return VolumeSummary{}
	}
	d := parseDFOutput(string(out))
	return VolumeSummary{
		Used:        d.used * 1024,
		Total:       d.total * 1024,
		UsedPercent: d.usedPct,
	}
}

type dfData struct {
	total   int64
	used    int64
	usedPct int
}

func parseDFOutput(raw string) dfData {
	lines := strings.Split(strings.TrimSpace(raw), "\n")
	if len(lines) < 2 {
		return dfData{}
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return dfData{}
	}
	total, _ := strconv.ParseInt(fields[1], 10, 64)
	used, _ := strconv.ParseInt(fields[2], 10, 64)
	pctStr := strings.TrimSuffix(fields[4], "%")
	pct, _ := strconv.Atoi(pctStr)
	return dfData{total: total, used: used, usedPct: pct}
}
