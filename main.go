package main

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/shirou/gopsutil/process"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var (
	cpuUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "Cpuinfo",
		Help: "CPU使用率",
	}, []string{"process"})

	memUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "Meminfo",
		Help: "内存使用率",
	}, []string{"process"})

	pidUsage = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "Pidinfo",
		Help: "进程pid",
	}, []string{"process"})

	// 使用互斥锁确保在更新指标时不被同时执行
	mutex sync.Mutex

	// 存储每个进程上次更新的时间戳
	lastUpdate = make(map[string]time.Time)
)

func init() {
	// Register metrics once
	prometheus.MustRegister(cpuUsage, memUsage, pidUsage)
}

func main() {
	processNames := os.Args[1:]
	if len(processNames) == 0 {
		fmt.Println("Usage: go run main.go <process1> <process2> ... <processN>")
		return
	}

	// 开启一个子协程执行更新指标逻辑
	go func() {
		for range time.Tick(time.Second * 5) { // 每隔 5 秒更新一次指标
			updateMetrics(processNames)
		}
	}()

	// 开启一个子协程定时打印 metrics 到控制台
	go func() {
		for range time.Tick(time.Second * 5) { // 每隔 5 秒打印一次
			printMetrics()
		}
	}()

	// Start HTTP server
	http.Handle("/metrics", promhttp.Handler())
	err := http.ListenAndServe("0.0.0.0:9100", nil)
	if err != nil {
		fmt.Printf("Error starting HTTP server: %s\n", err)
	}
}

func updateMetrics(processNames []string) {
	// 使用互斥锁确保在更新指标时不被同时执行
	mutex.Lock()
	defer mutex.Unlock()

	for _, processName := range processNames {
		// 获取进程的 PID
		pid := getPID(processName)
		if pid == 0 {
			// 如果进程不存在，设置指标为 0 表示未知值
			cpuUsage.WithLabelValues(processName).Set(float64(0)) // NaN
			memUsage.WithLabelValues(processName).Set(float64(0)) // NaN
			pidUsage.WithLabelValues(processName).Set(float64(0)) // NaN
			continue
		}

		lastUpdateTime, ok := lastUpdate[processName]
		// 检查是否需要更新，避免在短时间内频繁更新导致数据丢失
		if !ok || time.Since(lastUpdateTime) >= time.Second*5 {
			p, err := process.NewProcess(int32(pid))
			if err != nil {
				fmt.Printf("Error getting process: %s\n", err)
				return
			}

			// 获取进程的 CPU 使用率
			cpuPercent, err := p.CPUPercent()
			if err != nil {
				fmt.Printf("Error getting CPU percent: %s\n", err)
				return
			}
			cpuUsage.WithLabelValues(processName).Set(cpuPercent)

			// 获取进程的 mem 使用率
			memoryPercent, err := p.MemoryPercent()
			if err != nil {
				fmt.Printf("Error getting mem percent: %s\n", err)
				return
			}
			memUsage.WithLabelValues(processName).Set(float64(memoryPercent))

			// 获取进程的 pid
			pidUsage.WithLabelValues(processName).Set(float64(pid))

			// 更新上次更新时间
			lastUpdate[processName] = time.Now()
		}
	}
}

func getPID(processName string) int {
	processes, err := process.Processes()
	if err != nil {
		fmt.Printf("Error getting processes: %s\n", err)
		return 0
	}

	for _, p := range processes {
		name, _ := p.Name()
		if name == processName {
			return int(p.Pid)
		}
	}
	fmt.Printf("Process with name %s not found\n", processName)
	return 0
}

func printMetrics() {
	// 使用互斥锁确保在清除指标和打印指标时不被同时执行
	mutex.Lock()
	defer mutex.Unlock()

	// 打印最新的指标
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		fmt.Printf("Error gathering metrics: %s\n", err)
		return
	}

	for _, mf := range mfs {
		for _, m := range mf.Metric {
			// 检查标签是否以 "go_" 开头
			if len(m.Label) > 0 && !strings.HasPrefix(*m.Label[0].Name, "go_") {
				fmt.Printf("Metric: %s - Value: %f\n", m, m.Gauge.GetValue())
			}
		}
	}
	fmt.Println("==========================================")
}
