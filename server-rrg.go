package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/sony/sonyflake"
)

var ApplicationDescription string = "Redis REST Gateway"
var BuildVersion string = "0.0.0a"

var Debug bool = false
var DryRun bool = false

type APICreateRequestStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIReadRequestStruct struct {
	Uid uint64 `json:"uid"`
	Key string `json:"key"`
}

type APIUpdateRequestStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIDeleteRequestStruct struct {
	Uid uint64 `json:"uid"`
	Key string `json:"key"`
}

type APICreateResponseStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIReadResponseStruct struct {
	Uid   uint64                 `json:"uid"`
	Key   string                 `json:"key"`
	Value string                 `json:"value"`
	Error APIResponseErrorStruct `json:"error"`
}

type APIUpdateResponseStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIDeleteResponseStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIResponseErrorStruct struct {
	Code    uint16 `json:"code"`
	Message string `json:"message"`
}

type ConfigurationStruct struct {
	Listen string `json:"listen"`
	// Database DatabaseStruct `json:"database"`
}

type handleSignalParamsStruct struct {
	httpServer http.Server
}

type MetricsStruct struct {
	Warnings int32
	Errors   int32
	Create   int32
	Read     int32
	Update   int32
	Delete   int32
	Started  time.Time
}

var Configuration = ConfigurationStruct{}
var handleSignalParams = handleSignalParamsStruct{}

var MetricsNotifierPeriod int = 60
var Metrics = MetricsStruct{
	Warnings: 0,
	Errors:   0,
	Create:   0,
	Read:     0,
	Update:   0,
	Delete:   0,
	Started:  time.Now(),
}

var ctx = context.Background()
var flake = sonyflake.NewSonyflake(sonyflake.Settings{})

var rdb *redis.Client

func handleSignal() {

	log.Debug().Msg("Initialising signal handling function")

	signalChannel := make(chan os.Signal)
	signal.Notify(signalChannel, os.Interrupt, syscall.SIGTERM)

	go func() {

		<-signalChannel

		err := handleSignalParams.httpServer.Shutdown(context.Background())

		if err != nil {
			log.Fatal().Err(err).Msgf("HTTP server Shutdown error")

		} else {
			log.Info().Msgf("HTTP server Shutdown complete")
		}

		err = rdb.Close()

		if err != nil {
			log.Fatal().Err(err).Msgf("Redis client shutdown error")

		} else {
			log.Info().Msgf("Redis client shutdown complete")
		}

		// duration := time.Now().Sub(Metrics.Started)
		log.Info().
			Int32("Warnings", Metrics.Warnings).
			Int32("Errors", Metrics.Errors).
			Int32("Create", Metrics.Create).
			Int32("Read", Metrics.Read).
			Int32("Update", Metrics.Update).
			Int32("Delete", Metrics.Delete).
			Msgf("Counters")

		log.Warn().Msg("SIGINT")
		os.Exit(0)

	}()
}

func metricsNotifier() {
	go func() {
		for {
			time.Sleep(60 * time.Second)
			// duration := time.Now().Sub(Metrics.Started)
			log.Info().
				Int32("Warnings", Metrics.Warnings).
				Int32("Errors", Metrics.Errors).
				Int32("Create", Metrics.Create).
				Int32("Read", Metrics.Read).
				Int32("Update", Metrics.Update).
				Int32("Delete", Metrics.Delete).
				Msgf("Counters")
		}
	}()
}

func handlerIndex(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprintf(rw, "%s v%s\n", html.EscapeString(ApplicationDescription), html.EscapeString(BuildVersion))
}

func handlerCreate(rw http.ResponseWriter, req *http.Request) {

	var APICreateRequest APICreateRequestStruct

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APICreateRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("Error while reading API Response")
	}

	log.Info().Msgf("Create payload: %s", APICreateRequest)

	uid, err := flake.NextID()
	if err != nil {
		log.Fatal().Err(err).Msgf("flake.NextID() failed")
	}

	APICreateRequest.Uid = uid

	result := rdb.Set(ctx, APICreateRequest.Key, APICreateRequest.Value, 0)
	log.Info().Msgf("Create result: %s", result)

	_ = atomic.AddInt32(&Metrics.Create, 1)

	response := &APICreateResponseStruct{
		Uid: uid,
		Key: APICreateRequest.Key,
	}

	response_json, err := json.Marshal(response)
	if err != nil {
		log.Fatal().Err(err).Msgf("APICreateResponseStruct marshall failed")
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerRead(rw http.ResponseWriter, req *http.Request) {

	var APIReadRequest APIReadRequestStruct
	var APIReadResponse APIReadResponseStruct

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIReadRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("Error while reading API Response")
	}

	log.Info().Msgf("Read payload: %s", APIReadRequest)

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("flake.NextID() failed")
	}

	APIReadRequest.Uid = uid

	result := rdb.Get(ctx, APIReadRequest.Key)
	log.Info().Msgf("Read result: %s", result)

	value, err := result.Result()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Can't fetch result after Redis Get")
		APIReadResponse.Error.Message = err.Error()
		APIReadResponse.Error.Code = 404
	}

	_ = atomic.AddInt32(&Metrics.Read, 1)

	APIReadResponse.Uid = uid
	APIReadResponse.Key = APIReadRequest.Key
	APIReadResponse.Value = value

	response_json, err := json.Marshal(APIReadResponse)
	if err != nil {
		log.Fatal().Err(err).Msgf("APIReadResponseStruct marshall failed")
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerUpdate(rw http.ResponseWriter, req *http.Request) {

	var APIUpdateRequest APIUpdateRequestStruct

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIUpdateRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("Error while reading API Response")
	}

	log.Info().Msgf("Update payload: %s", APIUpdateRequest)

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("flake.NextID() failed")
	}

	APIUpdateRequest.Uid = uid

	result := rdb.Set(ctx, APIUpdateRequest.Key, APIUpdateRequest.Value, 0)
	log.Info().Msgf("Update result: %s", result)

	_ = atomic.AddInt32(&Metrics.Update, 1)

	response := &APIUpdateResponseStruct{
		Uid:   uid,
		Key:   APIUpdateRequest.Key,
		Value: APIUpdateRequest.Value,
	}

	response_json, err := json.Marshal(response)
	if err != nil {
		log.Fatal().Err(err).Msgf("APIUpdateResponseStruct marshall failed")
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerDelete(rw http.ResponseWriter, req *http.Request) {

	var APIDeleteRequest APIDeleteRequestStruct

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIDeleteRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("Error while reading API Response")
	}

	log.Info().Msgf("Del payload: %s", APIDeleteRequest)

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Fatal().Err(err).Msgf("flake.NextID() failed")
	}

	APIDeleteRequest.Uid = uid

	result := rdb.Del(ctx, APIDeleteRequest.Key)
	log.Info().Msgf("Del result: %s", result)

	_ = atomic.AddInt32(&Metrics.Delete, 1)

	response := &APIDeleteResponseStruct{
		Uid: uid,
		Key: APIDeleteRequest.Key,
		// Value: APIDeleteRequest.Value,
	}

	response_json, err := json.Marshal(response)
	if err != nil {
		log.Fatal().Err(err).Msgf("APIDeleteResponseStruct marshall failed")
	}

	fmt.Fprintf(rw, string(response_json))
}

func main() {

	bindPtr := flag.String("bind", "127.0.0.1:8080", "Address and port to listen")
	redisAddressPtr := flag.String("redis-address", "127.0.0.1:6379", "Address and port of the Redis server")
	redisDbPtr := flag.Int("redis-db", 0, "Redis database id")
	enableDebugPtr := flag.Bool("debug", false, "Enable verbose output")
	// enableDryRunPtr := flag.Bool("dry-run", false, "Dry run")
	showVersionPtr := flag.Bool("version", false, "Show version")

	flag.Parse()

	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	log.Debug().Msg("Logger initialised")

	handleSignal()
	metricsNotifier()

	if *enableDebugPtr {
		Debug = true
	}

	// if *enableDryRunPtr {
	// 	log.Print("This is a dry run")
	// 	DryRun = true
	// }

	if *showVersionPtr {
		fmt.Printf("%s\n", ApplicationDescription)
		fmt.Printf("Version: %s\n", BuildVersion)
		os.Exit(0)
	}

	redis_address, err := net.ResolveTCPAddr("tcp4", *redisAddressPtr)
	if err != nil {
		log.Fatal().Err(err).Msgf("Error while resolving Redis server address")
	}

	log.Info().Msgf("Redis server address %s", redis_address.String())

	rdb = redis.NewClient(&redis.Options{
		Addr:     redis_address.String(),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       *redisDbPtr,
		PoolSize: 2000,
	})

	listen_address, err := net.ResolveTCPAddr("tcp4", *bindPtr)
	if err != nil {
		log.Fatal().Err(err).Msgf("Error while resolving bind address")
	}

	log.Info().Msgf("Listening on %s", listen_address.String())
	Configuration.Listen = listen_address.String()

	srv := &http.Server{
		Addr:         Configuration.Listen,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	handleSignalParams.httpServer = *srv

	http.HandleFunc("/", handlerIndex)
	http.HandleFunc("/create", handlerCreate)
	http.HandleFunc("/read", handlerRead)
	http.HandleFunc("/update", handlerUpdate)
	http.HandleFunc("/delete", handlerDelete)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msgf("HTTP server ListenAndServe error")
	}

}
