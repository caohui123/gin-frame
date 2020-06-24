package main

import (
	"fmt"
	"gin-frame/libraries/config"
	"gin-frame/libraries/endless"
	"gin-frame/libraries/util/error"
	"gin-frame/routers"
	"log"
	"runtime"
	"strconv"
	"syscall"
)

var (
	port        int
	productName string
	moduleName  string
	env         string
)

func init() {
	runtime.GOMAXPROCS(runtime.NumCPU())
}

func main() {
	appSection := "app"
	appConfig := config.GetConfig("app", appSection)
	port, err := appConfig.Key("port").Int()
	error.Must(err)
	env = appConfig.Key("env").String()
	productName = appConfig.Key("product").String()
	moduleName = appConfig.Key("module").String()

	server := routers.InitRouter(port, productName, moduleName, env)

	tmpServer := endless.NewServer(fmt.Sprintf(":%s", strconv.Itoa(port)), server)
	tmpServer.BeforeBegin = func(add string) {
		log.Printf("Actual pid is %d", syscall.Getpid())
	}
	err = tmpServer.ListenAndServe()
	if err != nil {
		log.Printf("Server err: %v", err)
	}
}
