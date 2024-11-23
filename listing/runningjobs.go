package listing

import (
	"sort"

	"brooce/config"
	"brooce/heartbeat"
	myredis "brooce/redis"
	"brooce/task"

	"github.com/go-redis/redis"
)

var redisClient = myredis.Get()
var redisHeader = config.Config.ClusterName

// SCAN takes about 0.5s per million total items in redis
// we skip it by guessing the possible working list names
// from worker heartbeat data
// this is much faster, but the prune functions still need
// the true list to find any zombie working lists
func RunningJobs(fast bool, queue string, workerName string) (jobs []*task.Task, err error) {
	jobs = []*task.Task{}

	var keys []string

	if fast {
		var workers []*heartbeat.HeartbeatType

		workers, err = RunningWorkers()
		if err != nil {
			return
		}

		for _, worker := range workers {
			for _, thread := range worker.Threads {
				keys = append(keys, thread.WorkingList())
			}
		}

	} else {
		// brooce:queue:par4:working:node-35.cache-wf-64-3-19647-par4-0
		keys, err = myredis.ScanKeys(redisHeader + ":queue:" + queue + ":working:" + workerName + "*")
		if err != nil {
			return
		}
	}

	if len(keys) == 0 {
		return
	}

	values := make([]*redis.StringCmd, len(keys))
	_, err = redisClient.Pipelined(func(pipe redis.Pipeliner) error {
		for i, key := range keys {
			// LIndex is O(N) for items in the middle of the list, but these
			// working lists are generally one element long.
			values[i] = pipe.LIndex(key, 0)
		}
		return nil
	})
	// it's possible for an item to vanish between the KEYS and LINDEX steps -- this is not fatal!
	if err == redis.Nil {
		err = nil
	}
	if err != nil {
		return
	}

	for i, value := range values {
		if value.Err() != nil {
			// possible to get a redis.Nil error here if a job vanished between the KEYS and LINDEX steps
			continue
		}
		job, err := task.NewFromJson(value.Val(), task.QueueNameFromRedisKey(keys[i]))
		if err != nil {
			continue
		}
		job.RedisKey = keys[i]
		jobs = append(jobs, job)
	}

	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].StartTime < jobs[j].StartTime
	})

	task.PopulateHasLog(jobs)
	return
}
