package heartbeat

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sync"
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
var RunningCounter = 0

type HeartbeatType struct {
	ProcName  string              `json:"procname"`
	Hostname  string              `json:"hostname"`
	IP        string              `json:"ip"`
	PID       int                 `json:"pid"`
	Timestamp int64               `json:"timestamp"`
	Threads   []config.ThreadType `json:"threads"`
	MemUsage  int             `json:"memusage"`
	CpuUsage  int		  `json:"cpuusage"`
	Running	  int   `json:"running"`
	HighLoad  bool   `json:"highload"`
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
				cpuusage = int(float64(cpuafter.User - cpubefore.User) / total*100)
			}
		}
	}
	highLoad = cpuusage >= 90 || memusage >= 75

	hb := &HeartbeatType{
		ProcName:  config.Config.ProcName,
		IP:        myip.PublicIPv4(),
		PID:       os.Getpid(),
		Timestamp: time.Now().Unix(),
		Threads:   config.Threads,
		MemUsage:  memusage,
		CpuUsage:  cpuusage,
		Running:   RunningCounter,
		HighLoad:  highLoad,
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