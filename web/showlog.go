package web

import (
	"fmt"
	"net/http"
)

type logOutputType struct {
	ListType  string
	QueueName string
	// RawJson for task
	Raw string
	Log   string
	// Jobs *task.Task
}

func showlogHandler(req *http.Request, rep *httpReply) (err error) {
	path := splitUrlPath(req.URL.Path)
	if len(path) < 2 {
		err = fmt.Errorf("Invalid path")
		return
	}

	jobId := path[1]

	output := &logOutputType{
		QueueName: "PlaceHolder",
		ListType:  "PlaceHolder",
		Raw:	   "TODO",
	}
	if len(path) > 3 {
		output.QueueName = path[3]
		output.ListType = path[2]
	}
	if item := req.FormValue("item"); item != "" {
		output.Raw = item

		if item2 := req.FormValue("clicked"); item2 == "retry" {
			err = retryOne(output.QueueName, output.ListType, item)
			if err != nil {
				return
			}
		}
	}

	output.Log, err = redisClient.Get(fmt.Sprintf("%s:jobs:%s:log", redisLogHeader, jobId)).Result()
	if err != nil {
		return
	}

	err = templates.ExecuteTemplate(rep, "showlog", output)
	return
}
