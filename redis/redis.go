// +build !cluster

package redis

import (
	"log"
	"sync"
	"time"

	"brooce/config"

	"github.com/go-redis/redis"
)

var redisClient *redis.Client
var once sync.Once

func Get() *redis.Client {
	once.Do(func() {
		threads := len(config.Threads) + 10
		if threads > 15 {
			threads = 15
		}

		network := "tcp"
		addr := config.Config.Redis.Host
		if config.Config.Redis.Socket != "" {
			network = "unix"
			addr = config.Config.Redis.Socket
		}

		redisClient = redis.NewClient(&redis.Options{
			Network:      network,
			Addr:         addr,
			Password:     config.Config.Redis.Password,
			MaxRetries:   10,
			PoolSize:     threads,
			DialTimeout:  10 * time.Second,
			ReadTimeout:  60 * time.Second,
			WriteTimeout: 10 * time.Second,
			PoolTimeout:  2 * time.Second,
			DB:           config.Config.Redis.DB,
		})

		for {
			err := redisClient.Ping().Err()
			if err == nil {
				break
			}
			log.Println("Can't reach redis at", config.Config.Redis.Host, "-- are your redis addr and password right?", err)
			time.Sleep(60 * time.Second)
		}

		log.Println("Connected to redis at", addr)
	})

	return redisClient
}

// in the past, this function would just keep running RPOPLPUSH until it got an error back
// this works until the list gets long: then you can get into a race where the delayed list
// is being both flushed and repopulated (by a worker thread) forever
func FlushList(src, dst string) error {
	redisClient := Get()
	length, err := redisClient.LLen(src).Result()
	if err != nil {
		return err
	}

	for i := int64(0); i < length; i++ {
		_, err = redisClient.RPopLPush(src, dst).Result()
		if err != nil {
			break
		}
	}

	if err == redis.Nil {
		err = nil
	}

	return err
}

func ScanKeys(match string) (keys []string, err error) {
	redisClient := Get()

	iter := redisClient.Scan(0, match, 10000).Iterator()
	for iter.Next() {
		keys = append(keys, iter.Val())
	}
	err = iter.Err()

	return
}
