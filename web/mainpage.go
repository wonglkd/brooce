package web

import (
	"net/http"

	"brooce/heartbeat"
	"brooce/listing"
	// "brooce/task"
)

type mainpageOutputType struct {
	Queues map[string]*listing.QueueInfoType
	// RunningJobs    []*task.Task
	RunningWorkers     []*heartbeat.HeartbeatType
	TotalThreads       int
	TotalActiveWorkers int
}

func mainpageHandler(req *http.Request, rep *httpReply) (err error) {
	output := &mainpageOutputType{}

	// TODO: Wrap this behind a config option, in case someone does want to see it on the same page.
	// output.RunningJobs, err = listing.RunningJobs(true)
	// if err != nil {
	// 	return
	// }
	output.RunningWorkers, err = listing.RunningWorkers()
	if err != nil {
		return
	}
	output.TotalActiveWorkers = 0
	for _, worker := range output.RunningWorkers {
		if worker.Running > 0 {
			output.TotalActiveWorkers += 1
		}
	}
	output.Queues, err = listing.Queues(false)
	if err != nil {
		return
	}

	// for _, worker := range output.RunningWorkers {
	// 	output.TotalThreads += len(worker.Threads)
	// }

	err = templates.ExecuteTemplate(rep, "mainpage", output)
	return
}
