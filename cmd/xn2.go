package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/txn2/xn2/pkg/xer"

	"github.com/gin-contrib/zap"
	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"
)

// main function
func main() {

	// init with env vars or flags
	portEnv, ok := os.LookupEnv("PORT")
	if ok != true {
		portEnv = "8080"
	}

	debugEnv, ok := os.LookupEnv("DEBUG")
	if ok != true {
		debugEnv = "false"
	}

	cfgEnv, ok := os.LookupEnv("CONFIG")
	if ok != true {
		cfgEnv = ""
	}

	port := flag.String("port", portEnv, "The port to listen on for HTTP requests.")
	debug := flag.String("debug", debugEnv, "debug mode true or false.")
	cfg := flag.String("config", cfgEnv, "path the configuration.")
	flag.Parse()

	if *debug == "true" {
		gin.SetMode(gin.DebugMode)
	}

	// logger configuration
	zapCfg := zap.NewProductionConfig()

	if *debug == "false" {
		gin.SetMode(gin.ReleaseMode)
		zapCfg.DisableCaller = true
		zapCfg.DisableStacktrace = true
	}

	logger, err := zapCfg.Build()
	if err != nil {
		fmt.Printf("Can not build logger: %s\n", err.Error())
		return
	}

	err = logger.Sync()
	if err != nil {
		fmt.Printf("WARNING: Error synchronizing logger: %s\n", err.Error())
	}

	// load xer
	xr, err := xer.NewRunner(xer.Config{
		CfgYamlFile: *cfg,
	})
	if err != nil {
		logger.Error("Error configuring the runner.", zap.Error(err))
		os.Exit(1)
	}

	logger.Info("before routine")

	// Run xer (in go routine)
	go func() {
		logger.Info("about to run")

		xerMessages, xerErrorMessages, err := xr.Run()
		if err != nil {
			logger.Error("Runner error.", zap.Error(err))
			os.Exit(1)
		}

		for {
			select {
			case msg := <-xerMessages:
				logger.Info("runner message", zap.String("msg", msg))
			case e := <-xerErrorMessages:
				logger.Error("runner error", zap.Error(e))
			}
		}

	}()

	// Web server
	r := gin.New()

	r.Use(ginzap.Ginzap(logger, time.RFC3339, true))

	// Prometheus Metrics
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	err = r.Run(":" + *port)
	if err != nil {
		logger.Error("Error starting xn2 service.", zap.Error(err))
	}
}

// getEnv gets an environment variable or sets a default if
// one does not exist.
func getEnv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}

	return value
}
