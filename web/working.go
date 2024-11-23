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
	QueueName      string
	Query          string
}

func workingHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	output := &workingOutputType{}

	queueName := "*"
	if len(path) >= 2 {
		queueName = path[1]
	}
	output.QueueName = queueName

	workerName := "*"
	if len(path) >= 3 {
		workerName = path[2]
	}

	output.RunningJobs, err = listing.RunningJobs(false, queueName, workerName)
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
