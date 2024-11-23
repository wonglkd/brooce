package heartbeat

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"brooce/config"
	"brooce/myip"
	myredis "brooce/redis"
	"brooce/util"

	"github.com/mackerelio/go-osstat/cpu"
	"github.com/mackerelio/go-osstat/memory"
)

var heartbeatEvery = 30 * time.Second
var assumeDeadAfter = 95 * time.Second

var redisClient = myredis.Get()
var once sync.Once
var highLoad = false
var RunningCounter = int64(0)
var workerStartTime int64
var binHash string

type HeartbeatType struct {
	ProcName        string              `json:"procname"`
	Hostname        string              `json:"hostname"`
	IP              string              `json:"ip"`
	PID             int                 `json:"pid"`
	Timestamp       int64               `json:"timestamp"`
	Threads         []config.ThreadType `json:"threads"`
	MemUsage        int                 `json:"memusage"`
	CpuUsage        int                 `json:"cpuusage"`
	Running         int64               `json:"running"`
	HighLoad        bool                `json:"highload"`
	WorkerStartTime int64               `json:"workerstarted"`
	ConfigFile      string              `json:"configfile"`
	BinHash         string              `json:"binhash"`
}

func (hb *HeartbeatType) HeartbeatAge() time.Duration {
	return time.Since(time.Unix(hb.Timestamp, 0))
}

func (hb *HeartbeatType) HeartbeatTooOld() bool {
	return hb.HeartbeatAge() > assumeDeadAfter
}

// if heartbeat is for worker on the same machine, we can determine
// if the PID corresponds to a running process
func (hb *HeartbeatType) IsLocalZombie() bool {
	if hb.IP != myip.PublicIPv4() {
		return false
	}

	if hb.PID == 0 || hb.PID == os.Getpid() {
		return false
	}

	return !util.ProcessExists(hb.PID)
}

func (hb *HeartbeatType) Queues() (queues map[string]int) {
	queues = map[string]int{}

	for _, thread := range hb.Threads {
		queues[thread.Queue] += 1
	}

	return
}

func (hb *HeartbeatType) HostnameShort() string {
	// Have this be a config value
	return strings.Replace(hb.Hostname, ".yoursubdomain.local", "", 1)
}

func makeHeartbeat() string {
	memusage := -1
	cpuusage := -1
	{
		memory, err := memory.Get()
		if err != nil {
			log.Println("Error getting memstats:", err)
		} else {
			memusage = int(memory.Used * 100 / memory.Total)
		}
	}
	{
		cpubefore, err := cpu.Get()
		if err != nil {
			log.Println("Error getting cpustats:", err)
		} else {
			time.Sleep(time.Duration(1) * time.Second)
			cpuafter, err := cpu.Get()
			if err != nil {
				log.Println("Error getting cpustats:", err)
			} else {
				total := float64(cpuafter.Total - cpubefore.Total)
				cpuusage = int(float64(cpuafter.User-cpubefore.User) / total * 100)
			}
		}
	}
	highLoad = cpuusage >= 90 || memusage >= 75

	hb := &HeartbeatType{
		ProcName:        config.Config.ProcName,
		IP:              myip.PublicIPv4(),
		PID:             os.Getpid(),
		Timestamp:       time.Now().Unix(),
		Threads:         config.Threads,
		MemUsage:        memusage,
		CpuUsage:        cpuusage,
		Running:         atomic.LoadInt64(&RunningCounter),
		HighLoad:        highLoad,
		ConfigFile:      config.BrooceConfigFile,
		WorkerStartTime: workerStartTime,
		BinHash:         binHash,
	}

	var err error
	hb.Hostname, err = os.Hostname()
	if err != nil {
		log.Println("Warning: Unable to determine machine hostname!")
	}

	bytes, err := json.Marshal(hb)
	if err != nil {
		log.Fatalln(err)
	}

	return string(bytes)
}

func Start() {
	initStats()

	// need to send a single heartbeat FOR SURE before we grab a job!
	heartbeat()

	go func() {
		for {
			time.Sleep(heartbeatEvery)
			heartbeat()
		}
	}()
}

func heartbeat() {
	key := fmt.Sprintf("%s:workerprocs", config.Config.ClusterName)
	err := redisClient.HSet(key, config.Config.ProcName, makeHeartbeat()).Err()
	if err != nil {
		log.Println("redis heartbeat error:", err)
	}
}

func HighLoad() bool {
	return highLoad
}

func initStats() {
	workerStartTime = time.Now().Unix()
	bytes, err := os.ReadFile("/tmp/brooce/brooce_hash")
	if err == nil {
		binHash = string(bytes[:7])
		log.Println("Binary hash: ", binHash)
	} else {
		log.Println("Failed to get binhash: ", err)
	}
}
