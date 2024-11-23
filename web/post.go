package web

import (
	"fmt"
	"net/http"

	myredis "brooce/redis"
	"brooce/task"
)

func retryOne(queueName string, listType string, item string) (err error) {
	removeKey := fmt.Sprintf("%s:queue:%s:%s", redisHeader, queueName, listType)
	pushKey := fmt.Sprintf("%s:queue:%s:pending", redisHeader, queueName)

	var count int64
	count, err = redisClient.LRem(removeKey, 1, item).Result()
	if err != nil {
		return
	}
	if count == 1 {
		// TODO: Make this a toggle
		// redisClient.LPush(pushKey, item)
		// at top
		redisClient.RPush(pushKey, item)
	}
	return
}

func retryHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 3 {
		err = fmt.Errorf("Invalid path")
		return
	}

	listType := path[1]
	queueName := path[2]

	if item := req.FormValue("item"); item != "" {
		err = retryOne(queueName, listType, item)
	}

	return
}

func deleteHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 3 {
		err = fmt.Errorf("Invalid path")
		return
	}

	listType := path[1]
	queueName := path[2]

	removeKey := fmt.Sprintf("%s:queue:%s:%s", redisHeader, queueName, listType)

	if item := req.FormValue("item"); item != "" {
		job, err2 := task.NewFromJson(item, queueName)
		if err2 == nil {
			redisClient.Del(job.LogKey())
		}
		err = redisClient.LRem(removeKey, 1, item).Err()
	}

	return
}

func retryAllHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 3 {
		err = fmt.Errorf("Invalid path")
		return
	}

	listType := path[1]
	queueName := path[2]

	removeKey := fmt.Sprintf("%s:queue:%s:%s", redisHeader, queueName, listType)
	pushKey := fmt.Sprintf("%s:queue:%s:pending", redisHeader, queueName)

	err = myredis.FlushList(removeKey, pushKey)
	return
}

func deleteAllHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 3 {
		err = fmt.Errorf("Invalid path")
		return
	}

	listType := path[1]
	queueName := path[2]
	removeKey := fmt.Sprintf("%s:queue:%s:%s", redisHeader, queueName, listType)

	err = redisClient.Del(removeKey).Err()
	return
}

func deleteAllWithLogsHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 3 {
		err = fmt.Errorf("Invalid path")
		return
	}

	listType := path[1]
	queueName := path[2]

	removeKey := fmt.Sprintf("%s:queue:%s:%s", redisHeader, queueName, listType)

	// Expire logs
	var jobs []string
	jobs, err = redisClient.LRange(removeKey, 0, -1).Result()
	if err != nil {
		return err
	}
	for _, value := range jobs {
		job, err := task.NewFromJson(value, queueName)
		if err != nil {
			continue
		}
		redisClient.Expire(job.LogKey(), 60)
	}

	err = redisClient.Del(removeKey).Err()
	return
}
