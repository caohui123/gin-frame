package redis

import (
	"context"
	"fmt"
	"github.com/opentracing/opentracing-go"
	"strings"
	"time"
	"gin-frame/libraries/config"
	"gin-frame/libraries/log"
	"gin-frame/libraries/util"
	"gin-frame/libraries/xhop"

	"github.com/gomodule/redigo/redis"
)

type RedisDB struct {
	pool   *redis.Pool
	Config *Config
}

var obj map[string]*RedisDB

func Conn(conn string) (db *RedisDB, err error) {
	if len(obj) == 0 {
		obj = make(map[string]*RedisDB)
	}
	if obj[conn] != nil {
		db = obj[conn]
		return
	}

	fileCfg := config.GetConfig("redis", conn)

	portCfg, err := fileCfg.Key("port").Int()
	dbCfg, err := fileCfg.Key("db").Int()
	maxActiveCfg, err := fileCfg.Key("max_active").Int();
	maxIdleCfg, err := fileCfg.Key("max_idle").Int();
	logCfg, err := fileCfg.Key("is_log").Bool()
	util.Must(err)

	cfg := &Config{
		Host:       fileCfg.Key("host").String(),
		Port:      	portCfg,
		Password:   fileCfg.Key("auth").String(),
		DB:         dbCfg,
		MaxActive:  maxActiveCfg,
		MaxIdle:    maxIdleCfg,
		IsLog:		logCfg,
	}
		
	db = new(RedisDB)
	db.Config = cfg
	db.pool = &redis.Pool{
		Dial: func() (redis.Conn, error) {
			return redis.Dial(
				"tcp",
				fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
				redis.DialPassword(cfg.Password),
				redis.DialDatabase(cfg.DB),
				redis.DialConnectTimeout(time.Second*2),
				redis.DialReadTimeout(time.Second*2),
				redis.DialWriteTimeout(time.Second*2),
			)
		},
		TestOnBorrow: func(c redis.Conn, t time.Time) error {
			_, err := c.Do("PING")
			return err
		},
		MaxIdle:     cfg.MaxIdle, // 最大的空闲连接数，表示即使没有redis连接时依然可以保持N个空闲的连接，而不被清除，随时处于待命状态
		MaxActive:   cfg.MaxActive,  // 最大的激活连接数，表示同时最多有N个连接 ，为0事表示没有限制
		IdleTimeout: time.Second, //最大的空闲连接等待时间，超过此时间后，空闲连接将被关闭
		Wait:        true, // 当链接数达到最大后是否阻塞，如果不的话，达到最大后返回错误
	}

	if _, err = db.Do(context.TODO(), "PING"); err != nil {
		fmt.Println(err)
		err = errorsWrap(err, "ping error")
		return
	}
	obj[conn] = db

	return
}

func errorsWrap(err error, msg string) error {
	return fmt.Errorf("%s: %w", msg, err)
}

// ConnPool 返回 redis.Pool.
// 除非必要一般不建议用这个函数, 用本库封装好的函数操作数据库.
func (db *RedisDB) ConnPool() *redis.Pool {
	return db.pool
}

// Close 释放连接资源.
func (db *RedisDB) Close() error {
	if db.pool != nil {
		return db.pool.Close()
	}
	return nil
}

// Do 执行 redis 命令
// NOTE 除非有必要(比如在一个函数内部需要执行多次 redis 操作), 否则请用该函数执行所有的操作, 这样能有效避免忘记释放资源.
func (db *RedisDB) Do(ctx context.Context, commandName string, args ...interface{}) (reply interface{}, err error) {
	if commandName == "PING" {
		return
	}
	var (
		conn       = db.pool.Get()
		parent     = opentracing.SpanFromContext(ctx)
		span       opentracing.Span
		startAt    = time.Now()
		endAt      time.Time
		logFormat  = log.LogHeaderFromContext(ctx)
		argsStr    []string
	)
	defer conn.Close()

	reply, err = conn.Do(commandName, args...)

	if logFormat == nil {
		logFormat = log.NewLog()
	}

	for _, arg := range args {
		argsStr = append(argsStr, fmt.Sprint(arg))
	}

	lastModule := logFormat.Module
	lastStartTime := logFormat.StartTime
	lastEndTime := logFormat.EndTime
	lastXHop := logFormat.XHop
	defer func() {
		logFormat.Module = lastModule
		logFormat.StartTime = lastStartTime
		logFormat.EndTime = lastEndTime
		logFormat.XHop = lastXHop
	}()

	defer func() {
		endAt = time.Now()
		logFormat.StartTime = startAt
		logFormat.EndTime = endAt
		latencyTime := logFormat.EndTime.Sub(logFormat.StartTime).Microseconds()// 执行时间
		logFormat.LatencyTime = latencyTime
		logFormat.XHop = xhop.NewXhopNull()

		logFormat.Module = "databus/redis"

		if err != nil {
			log.Errorf(logFormat, "redis do:[%s], error: %s",
				fmt.Sprint(commandName, " ", strings.Join(argsStr, " ")),
				err)
		}else if db.Config.IsLog == true {
			log.Infof(logFormat, "redis do:[%s], used: %d milliseconds",
				fmt.Sprint(commandName, " ", strings.Join(argsStr, " ")),
				endAt.Sub(startAt).Milliseconds())
		}
	}()

	if parent == nil {
		span = opentracing.StartSpan("redisDo")
	} else {
		span = opentracing.StartSpan("redisDo", opentracing.ChildOf(parent.Context()))
	}
	defer span.Finish()

	span.SetTag("db.type", "redis")
	span.SetTag("db.statement", fmt.Sprint(commandName, " ", strings.Join(argsStr, " ")))
	span.SetTag("error", err != nil)
	return
}


