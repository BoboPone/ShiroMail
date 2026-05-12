package database

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/go-redis/redis/v8"
)

func NewRedis(addr string, password string) *redis.Client {
	db := 0
	if slash := strings.LastIndex(addr, "/"); slash > strings.LastIndex(addr, ":") {
		if parsed, err := strconv.Atoi(strings.TrimSpace(addr[slash+1:])); err == nil {
			db = parsed
			addr = addr[:slash]
		}
	}

	return redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		PoolSize:     20,
		MinIdleConns: 5,
		MaxRetries:   3,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
	})
}

func PingRedis(ctx context.Context, client *redis.Client) error {
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return client.Ping(pingCtx).Err()
}
