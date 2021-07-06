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
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIUpdateResponseStruct struct {
	Uid   uint64 `json:"uid"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

type APIDeleteResponseStruct struct {
	Uid uint64 `json:"uid"`
	Key string `json:"key"`
}

type ConfigurationStruct struct {
	Listen string `json:"listen"`
	// Redis RedisStruct `json:"redis"`
}

type handleSignalParamsStruct struct {
	httpServer http.Server
}

type MetricsStruct struct {
	Index    int32
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
	Index:    0,
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
	_ = atomic.AddInt32(&Metrics.Index, 1)
	fmt.Fprintf(rw, "%s v%s\n", html.EscapeString(ApplicationDescription), html.EscapeString(BuildVersion))
}

func handlerMetrics(rw http.ResponseWriter, req *http.Request) {

	fmt.Fprintf(rw, "# TYPE redis_rest_gw_requests counter\n")
	fmt.Fprintf(rw, "# HELP Number of the requests to the REST Gateway by type\n")
	fmt.Fprintf(rw, "redis_rest_gw_requests{method=\"create\"} %v\n", Metrics.Create)
	fmt.Fprintf(rw, "redis_rest_gw_requests{method=\"read\"} %v\n", Metrics.Read)
	fmt.Fprintf(rw, "redis_rest_gw_requests{method=\"update\"} %v\n", Metrics.Update)
	fmt.Fprintf(rw, "redis_rest_gw_requests{method=\"delete\"} %v\n", Metrics.Delete)

	fmt.Fprintf(rw, "# TYPE redis_rest_gw_errors counter\n")
	fmt.Fprintf(rw, "# HELP Number of the raised errors\n")
	fmt.Fprintf(rw, "redis_rest_gw_errors %v\n", Metrics.Errors)

	fmt.Fprintf(rw, "# TYPE redis_rest_gw_warnings counter\n")
	fmt.Fprintf(rw, "# HELP Number of the raised warnings\n")
	fmt.Fprintf(rw, "redis_rest_gw_warnings %v\n", Metrics.Warnings)

	fmt.Fprintf(rw, "# TYPE redis_rest_gw_index counter\n")
	fmt.Fprintf(rw, "# HELP Number of the requests to /\n")
	fmt.Fprintf(rw, "redis_rest_gw_index %v\n", Metrics.Index)

}

func handlerCreate(rw http.ResponseWriter, req *http.Request) {

	var APICreateRequest APICreateRequestStruct
	var APICreateResponse APICreateResponseStruct

	if req.Method != "POST" {
		log.Warn().Msgf("Ignoring unsupported http method %s", req.Method)
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APICreateRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Error while JSON decoding the API request")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Info().Msgf("Create payload: %+v", APICreateRequest)

	if APICreateRequest.Key == "" {
		log.Warn().Msgf("Field 'key' not found or empty in request, dismissing.")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	uid, err := flake.NextID()
	if err != nil {
		log.Err(err).Msgf("flake.NextID() failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	APICreateRequest.Uid = uid

	result := rdb.Set(ctx, APICreateRequest.Key, APICreateRequest.Value, 0)
	log.Info().Msgf("Create result: %s", result)

	_ = atomic.AddInt32(&Metrics.Create, 1)

	APICreateResponse.Uid = uid
	APICreateResponse.Key = APICreateRequest.Key
	APICreateResponse.Value = APICreateRequest.Value

	response_json, err := json.Marshal(APICreateResponse)
	if err != nil {
		log.Err(err).Msgf("APICreateResponseStruct marshall failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerRead(rw http.ResponseWriter, req *http.Request) {

	var APIReadRequest APIReadRequestStruct
	var APIReadResponse APIReadResponseStruct

	_ = atomic.AddInt32(&Metrics.Read, 1)

	if req.Method != "POST" {
		log.Warn().Msgf("Ignoring unsupported http method %s", req.Method)
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIReadRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Error while JSON decoding the API request")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Info().Msgf("Read payload: %+v", APIReadRequest)

	if APIReadRequest.Key == "" {
		log.Warn().Msgf("Field 'key' not found or empty in request, dismissing.")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("flake.NextID() failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	APIReadRequest.Uid = uid

	result := rdb.Get(ctx, APIReadRequest.Key)
	log.Info().Msgf("Read result: %+v", result)

	value, err := result.Result()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Can't fetch result after Redis Get")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	APIReadResponse.Uid = uid
	APIReadResponse.Key = APIReadRequest.Key
	APIReadResponse.Value = value

	response_json, err := json.Marshal(APIReadResponse)
	if err != nil {
		log.Err(err).Msgf("APICreateResponseStruct marshall failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerUpdate(rw http.ResponseWriter, req *http.Request) {

	var APIUpdateRequest APIUpdateRequestStruct
	var APIUpdateResponse APIUpdateResponseStruct

	_ = atomic.AddInt32(&Metrics.Update, 1)

	if req.Method != "POST" {
		log.Warn().Msgf("Ignoring unsupported http method %s", req.Method)
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIUpdateRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Error while JSON decoding the API request")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Info().Msgf("Update payload: %+v", APIUpdateRequest)

	if APIUpdateRequest.Key == "" {
		log.Warn().Msgf("Field 'key' not found or empty in request, dismissing.")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("flake.NextID() failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	APIUpdateRequest.Uid = uid

	result := rdb.Set(ctx, APIUpdateRequest.Key, APIUpdateRequest.Value, 0)
	log.Info().Msgf("Update result: %+v", result)

	APIUpdateResponse.Uid = uid
	APIUpdateResponse.Key = APIUpdateRequest.Key
	APIUpdateResponse.Value = APIUpdateRequest.Value

	response_json, err := json.Marshal(APIUpdateResponse)
	if err != nil {
		log.Err(err).Msgf("APICreateResponseStruct marshall failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	fmt.Fprintf(rw, string(response_json))
}

func handlerDelete(rw http.ResponseWriter, req *http.Request) {

	var APIDeleteRequest APIDeleteRequestStruct
	var APIDeleteResponse APIDeleteResponseStruct

	_ = atomic.AddInt32(&Metrics.Delete, 1)

	if req.Method != "POST" {
		log.Warn().Msgf("Ignoring unsupported http method %s", req.Method)
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	JSONDecoder := json.NewDecoder(req.Body)
	err := JSONDecoder.Decode(&APIDeleteRequest)
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("Error while JSON decoding the API request")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	log.Info().Msgf("Del payload: %+v", APIDeleteRequest)

	if APIDeleteRequest.Key == "" {
		log.Warn().Msgf("Field 'key' not found or empty in request, dismissing.")
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	uid, err := flake.NextID()
	if err != nil {
		_ = atomic.AddInt32(&Metrics.Errors, 1)
		log.Err(err).Msgf("flake.NextID() failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	APIDeleteRequest.Uid = uid

	result := rdb.Del(ctx, APIDeleteRequest.Key)
	log.Info().Msgf("Del result: %+v", result)

	APIDeleteResponse.Uid = uid
	APIDeleteResponse.Key = APIDeleteRequest.Key

	response_json, err := json.Marshal(APIDeleteResponse)
	if err != nil {
		log.Err(err).Msgf("APICreateResponseStruct marshall failed")
		rw.WriteHeader(http.StatusInternalServerError)
		return
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

	if Debug {
		metricsNotifier()
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
	http.HandleFunc("/metrics", handlerMetrics)
	http.HandleFunc("/create", handlerCreate)
	http.HandleFunc("/read", handlerRead)
	http.HandleFunc("/update", handlerUpdate)
	http.HandleFunc("/delete", handlerDelete)

	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatal().Err(err).Msgf("HTTP server ListenAndServe error")
	}

}
