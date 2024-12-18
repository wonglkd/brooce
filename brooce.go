package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"brooce/config"
	"brooce/cron/cronsched"
	"brooce/heartbeat"
	"brooce/lock"
	"brooce/prune"
	myredis "brooce/redis"
	"brooce/requeue"
	"brooce/runnabletask"
	mysignals "brooce/signals"
	"brooce/suicide"
	tasklib "brooce/task"
	"brooce/web"

	"github.com/go-redis/redis"
	daemon "github.com/sevlyar/go-daemon"
)

var redisClient = myredis.Get()
var redisHeader = config.Config.ClusterName
var threadWg sync.WaitGroup

var daemonizeOpt = flag.Bool("daemonize", false, "Detach and run in the background!")
var helpOpt = flag.Bool("help", false, "Show these options!")

func main() {
	flag.Parse()
	if *helpOpt {
		flag.PrintDefaults()
		os.Exit(0)
	}

	if *daemonizeOpt {
		context := &daemon.Context{
			LogFileName: filepath.Join(config.BrooceLogDir, "brooce.log"),
			LogFilePerm: 0644,
		}
		child, err := context.Reborn()
		if err != nil {
			log.Fatalln("Daemonize error:", err)
		}

		if child != nil {
			log.Println("Starting brooce in the background..")
			os.Exit(0)
		} else {
			defer context.Release()
		}
	}

	heartbeat.Start()
	web.Start()
	cronsched.Start()
	if config.Config.NoPrune {
		log.Println("Pruning disabled!")
	} else {
		log.Println("Pruning enabled")
		prune.Start()
	}
	requeue.Start()
	suicide.Start()
	lock.Start()
	mysignals.Start()

	for _, thread := range config.Threads {
		threadWg.Add(1)
		go runner(thread)
		// Stagger starting of threads.
		time.Sleep(time.Duration(5) * time.Second)
	}

	if len(config.Threads) > 0 {
		log.Println("Started with queues:", config.ThreadString)
	} else {
		log.Println("Started with NO queues! We won't be doing any jobs!")
	}

	mysignals.WaitForShutdownRequest()
	log.Println("Shutdown requested! Waiting for all threads to finish (repeat signal to skip)..")
	threadWg.Wait()
	log.Println("Exiting..")
}

func correctListLength(fromList string, toList string, maxLen int64) bool {
	// thread.WorkingList() should have 1 item now
	// if it has less or more, something went wrong!
	length := redisClient.LLen(fromList)
	if length.Err() != nil {
		log.Println("Error while checking length of", fromList, ":", length.Err())
		return true
	}
	if length.Val() > maxLen {
		log.Println(fromList, "should have length ", maxLen, " but has length", length.Val(), "! It'll be flushed to", toList)
		if err := myredis.FlushList(fromList, toList); err != nil {
			log.Println("Error while flushing", fromList, "to", toList, ":", err)
		}
		return true
	}
	return false
}

func runner(thread config.ThreadType) {
	var threadOutputLog *os.File
	if config.Config.FileOutputLog.Enable {
		var err error
		filename := filepath.Join(config.BrooceLogDir, fmt.Sprintf("%s-%s-%d.log", config.Config.ClusterName, thread.Queue, thread.Id))
		threadOutputLog, err = os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
		if err != nil {
			log.Fatalln("Unable to open logfile", filename, "for writing! Error was", err)
		}
		defer threadOutputLog.Close()
	}

	for {
		if mysignals.WasShutdownRequested() {
			threadWg.Done()
			return
		}

		// We should not be working on anything right now. Correct before we wait.
		if correctListLength(thread.WorkingList(), thread.PendingList(), 0) {
			continue
		}

		// Wait till CPU / memory usage is < 90%.
		if heartbeat.HighLoad() {
			log.Println("Waiting till we are no longer under HighLoad:", thread.Queue, thread.Id)
		}
		for heartbeat.HighLoad() {
			time.Sleep(time.Duration(30) * time.Second)
		}
		// If there are running jobs on this host, sleep to give other workers a chance to pick them up
		// Simple attempt at load balancing
		waitTimeout := 15 * time.Second
		if atomic.LoadInt64(&heartbeat.RunningCounter) > 0 {
			time.Sleep(time.Duration(60) * time.Second)
			waitTimeout = 5 * time.Second
		}

		taskStr, err := redisClient.BRPopLPush(thread.PendingList(), thread.WorkingList(), waitTimeout).Result()
		if err != nil {
			if err != redis.Nil {
				log.Println("redis error while running BRPOPLPUSH:", err)
			}
			continue
		}

		if correctListLength(thread.WorkingList(), thread.PendingList(), 1) {
			continue
		}

		atomic.AddInt64(&heartbeat.RunningCounter, 1)

		var exitCode int
		task, err := tasklib.NewFromJson(taskStr, thread.Queue)
		if err != nil {
			log.Println("Failed to decode task:", err)
		} else {
			task.RedisKey = thread.WorkingList()
			rTask := &runnabletask.RunnableTask{
				Task:       task,
				FileWriter: threadOutputLog,
			}
			suicide.ThreadIsWorking(thread.Name)
			exitCode, err = rTask.Run()
			suicide.ThreadIsWaiting(thread.Name)

			if err != nil && !strings.HasPrefix(err.Error(), "timeout after") && !strings.HasPrefix(err.Error(), "exit status") {
				log.Printf("Error in task %v: %v", rTask.Id, err)
			}
		}

		_, err = redisClient.Pipelined(func(pipe redis.Pipeliner) error {

			// Unix standard "temp fail" code
			if exitCode == 75 {
				// DELAYED
				if !task.KillOnDelay() {
					pipe.LPush(thread.DelayedList(), task.Json())
				}

			} else if (err != nil || exitCode != 0) && !task.NoFail() {
				// FAILED
				if task.MaxTries() > task.Tried && exitCode != 65 {
					// Designate error code 65 for do not retry (EX_DATAERR in Unix).
					pipe.LPush(thread.DelayedList(), task.Json())
				} else {
					if task.RedisLogFailedExpireAfter() > 0 {
						pipe.Expire(task.LogKey(), time.Duration(task.RedisLogFailedExpireAfter())*time.Second)
					}

					if !task.DropOnFail() {
						pipe.LPush(thread.FailedList(), task.Json())
					}

					if task.NoRedisLogOnFail() && task.LogKey() != "" {
						pipe.Del(task.LogKey())
					}
				}

			} else {
				// DONE

				if !task.DropOnSuccess() {
					pipe.LPush(thread.DoneList(), task.Json())
				}

				pipe.Expire(task.LogKey(), time.Duration(48)*time.Hour)

				if task.NoRedisLogOnSuccess() && task.LogKey() != "" {
					pipe.Del(task.LogKey())
				}
			}

			pipe.LPop(thread.WorkingList())

			atomic.AddInt64(&heartbeat.RunningCounter, -1)

			return nil
		})

		if err != nil {
			log.Println("Error while pipelining job from", thread.WorkingList(), ":", err)
		}

	}
}
