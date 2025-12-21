package main

import (
	"fmt"
	"time"
)

// FormatStreak formats a duration as "up 10mo 2d" or "down 2h 15m"
func FormatStreak(d time.Duration, up bool) string {
	prefix := "up"
	if !up {
		prefix = "down"
	}
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%s %ds", prefix, int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%s %dm", prefix, int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%s %dh %dm", prefix, int(d.Hours()), int(d.Minutes())%60)
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%s %dd %dh", prefix, int(d.Hours()/24), int(d.Hours())%24)
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%s %dmo %dd", prefix, int(d.Hours()/24/30), int(d.Hours()/24)%30)
	default:
		return fmt.Sprintf("%s %dy %dmo", prefix, int(d.Hours()/24/365), int(d.Hours()/24/30)%12)
	}
}

// GenerateSparkline creates an ASCII sparkline from latency values
func GenerateSparkline(latencies []int, width int) string {
	if len(latencies) == 0 {
		return ""
	}
	blocks := []rune("▁▂▃▄▅▆▇█")
	min, max := latencies[0], latencies[0]
	for _, l := range latencies {
		if l < min {
			min = l
		}
		if l > max {
			max = l
		}
	}
	buckets, counts := make([]int, width), make([]int, width)
	for i, l := range latencies {
		idx := i * width / len(latencies)
		if idx >= width {
			idx = width - 1
		}
		buckets[idx] += l
		counts[idx]++
	}
	result := make([]rune, width)
	spread := max - min
	if spread == 0 {
		spread = 1
	}
	for i := range width {
		if counts[i] == 0 {
			result[i] = blocks[0]
			continue
		}
		avg := buckets[i] / counts[i]
		idx := int(float64(avg-min) / float64(spread) * float64(len(blocks)-1))
		if idx >= len(blocks) {
			idx = len(blocks) - 1
		}
		result[i] = blocks[idx]
	}
	return string(result)
}

// FormatUptime formats uptime percentage
func FormatUptime(pct float64) string {
	if pct == 100 {
		return "100%"
	}
	return fmt.Sprintf("%.1f%%", pct)
}

// FormatLatency formats latency in ms
func FormatLatency(ms int) string {
	if ms == 0 {
		return "-"
	}
	return fmt.Sprintf("%dms", ms)
}

// MinMaxPair holds min and max values
type MinMaxPair struct{ Min, Max int }

// MinMax returns min and max from a slice
func MinMax(values []int) MinMaxPair {
	if len(values) == 0 {
		return MinMaxPair{}
	}
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	return MinMaxPair{min, max}
}
