package web

import (
	"net/http"

	"brooce/heartbeat"
	"brooce/listing"
	"brooce/task"
)

type workingOutputType struct {
	RunningJobs    []*task.Task
	RunningWorkers []*heartbeat.HeartbeatType
	TotalThreads   int
}

func workingHandler(req *http.Request, rep *httpReply) (err error) {
	output := &workingOutputType{}

	output.RunningJobs, err = listing.RunningJobs(true)
	if err != nil {
		return
	}
	output.RunningWorkers, err = listing.RunningWorkers()
	if err != nil {
		return
	}

	for _, worker := range output.RunningWorkers {
		output.TotalThreads += len(worker.Threads)
	}

	err = templates.ExecuteTemplate(rep, "working", output)
	return
}
