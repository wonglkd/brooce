package runnabletask

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"brooce/config"
	"brooce/lock"
	myredis "brooce/redis"
	tasklib "brooce/task"
)

var tsFormat = "2006-01-02 15:04:05"

var redisClient = myredis.Get()
var redisHeader = config.Config.ClusterName

type RunnableTask struct {
	*tasklib.Task
	FileWriter io.Writer

	buffer     *bytes.Buffer
	bufferLock sync.Mutex
	running    bool
	Pid        int
}

// exists returns whether the given file or directory exists
func exists(path string) (bool, error) {
    _, err := os.Stat(path)
    if err == nil { return true, nil }
    if os.IsNotExist(err) { return false, nil }
    return false, err
}


func KillFileExists() bool {
	v, _ := exists(config.BrooceConfigDir + "/killall")
	return v
}

func (task *RunnableTask) Run() (exitCode int, err error) {
	if len(task.Command) == 0 {
		return
	}

	if task.Id == "" {
		err = task.GenerateId()
		if err != nil {
			err = fmt.Errorf("Error in task.GenerateId: %v", err)
			return
		}
	}

	starttime := time.Now()
	task.StartTime = starttime.Unix()
	err = redisClient.LSet(task.RedisKey, 0, task.Json()).Err()
	if err != nil {
		err = fmt.Errorf("Error updating working key 0: %v", err)
		return
	}

	var gotLock bool
	gotLock, err = lock.GrabLocks(task.Locks, task.WorkerThreadName())
	if err != nil {
		err = fmt.Errorf("Error grabbing locks: %v", err)
		return
	}
	if !gotLock {
		exitCode = 75
		return
	}
	defer lock.ReleaseLocks(task.Locks, task.WorkerThreadName())

	task.Tried += 1

	task.StartFlushingLog()
	defer func() {
		finishtime := time.Now()
		runtime := finishtime.Sub(starttime)

		task.WriteLog(fmt.Sprintf("*** COMPLETED_AT:[%s] RUNTIME:[%s] EXITCODE:[%d] ERROR:[%v]\n",
			finishtime.Format(tsFormat),
			runtime,
			exitCode,
			err,
		))
		task.StopFlushingLog()
	}()

	task.WriteLog(fmt.Sprintf("\n\n*** COMMAND:[%s]\n STARTED_AT:[%s] WORKER_THREAD:[%s] QUEUE:[%s]\n",
		task.Command,
		starttime.Format(tsFormat),
		task.WorkerThreadName(),
		task.QueueName(),
	))

	cmd := exec.Command("bash", "-c", task.Command)
	cmd.Stdout = &runnableTaskStdoutLog{RunnableTask: task}
	cmd.Stderr = cmd.Stdout

	// give process its own PGID so we can kill its children as well below
	// https://medium.com/@felixge/killing-a-child-process-and-all-of-its-children-in-go-54079af94773
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	done := make(chan error)
	err = cmd.Start()
	if err != nil {
		return
	}

	task.Pid = cmd.Process.Pid
	task.WriteLog(fmt.Sprintf("\n*** PID: [%d]\n", task.Pid))

	go func() {
		done <- cmd.Wait()
	}()

	go func() {
		for task.running {
			if task.ToKill || KillFileExists() {
				syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
				done <- fmt.Errorf("killed on command")
				return
			}
			time.Sleep(5 * time.Second)
			// Check if to kill has been set
		}
	}()

	timeout := task.TimeoutDuration()

	select {
	case err = <-done:
		//finished normally, do nothing!
	case <-time.After(timeout):
		//timed out!
		syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		err = fmt.Errorf("timeout after %v", timeout)
	}

	task.EndTime = time.Now().Unix()

	if msg, ok := err.(*exec.ExitError); ok {
		exitCode = msg.Sys().(syscall.WaitStatus).ExitStatus()
	}

	return
}
