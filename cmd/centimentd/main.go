package main

import (
	"context"
	"fmt"
	stdlog "log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/elithrar/centiment"
	"github.com/gorilla/mux"
	"github.com/oklog/run"
	"github.com/pkg/errors"
	"github.com/rs/cors"

	"cloud.google.com/go/firestore"
	nl "cloud.google.com/go/language/apiv1"
	"github.com/ChimeraCoder/anaconda"
	"github.com/go-kit/kit/log"
)

func main() {
	// Logging
	logger := log.With(
		log.NewLogfmtLogger(log.NewSyncWriter(os.Stdout)),
		"ts", log.DefaultTimestampUTC,
	)
	stdlog.SetOutput(log.NewStdlibAdapter(logger))

	conf, err := parseConfig()
	if err != nil {
		fatal(logger, err)
	}

	logger.Log(
		"msg", fmt.Sprintf("using config file at %s", conf.searchConfigPath),
	)

	// Cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Datastore
	fs, err := firestore.NewClient(ctx, conf.projectID)
	if err != nil {
		fatal(logger, err)
	}

	store := &centiment.Firestore{
		Store:          fs,
		CollectionName: "sentiments",
	}

	// Application server
	env := &centiment.Env{DB: store, Logger: logger, Hostname: conf.hostname}
	router := mux.NewRouter().StrictSlash(true)
	router.Use(centiment.LogRequest(
		log.With(logger, "worker", "web"),
	))
	router.Use(cors.Default().Handler)
	centiment.AddIndexEndpoints(router, env)
	centiment.AddHealthCheckEndpoints(router, env)
	centiment.AddMetricEndpoints(router, env)
	centiment.AddSentimentEndpoints(router, env)
	srv := &http.Server{
		Addr:         conf.listenAddress,
		WriteTimeout: time.Second * 15,
		ReadTimeout:  time.Second * 15,
		IdleTimeout:  time.Second * 60,
		Handler:      router,
	}

	terms, err := parseSearchTerms(conf.searchConfigPath)
	if err != nil {
		fatal(logger, err)
	}
	anaconda.SetConsumerKey(conf.consumerKey)
	anaconda.SetConsumerSecret(conf.consumerSecret)
	twitterAPI := anaconda.NewTwitterApi(conf.accessToken, conf.accessSecret)

	nlClient, err := nl.NewClient(ctx)
	if err != nil {
		fatal(logger, err)
	}

	// Initialize worker pools.
	searcher, err := centiment.NewSearcher(
		log.With(logger, "worker", "searcher"),
		terms,
		conf.maxTweets,
		time.Minute*15, // TODO(matt): Make this configurable
		twitterAPI,
		store,
	)
	if err != nil {
		fatal(logger, err)
	}

	analyzer, err := centiment.NewAnalyzer(
		log.With(logger, "worker", "analyzer"),
		nlClient,
		conf.numWorkers,
	)
	if err != nil {
		fatal(logger, err)
	}

	aggregator, err := centiment.NewAggregator(
		log.With(logger, "worker", "aggregator"),
		store,
	)
	if err != nil {
		fatal(logger, err)
	}

	ticker := time.NewTicker(conf.runInterval)

	// Run worker pools.
	var group run.Group
	group.Add(
		centiment.RunServer(
			srv,
		),
		func(err error) {
			cancel()
			srv.Shutdown(ctx)
		},
	)
	group.Add(
		signalHandler(ctx),
		func(err error) {
			cancel()
		},
	)
	group.Add(
		runAnalysis(
			ctx,
			logger,
			ticker,
			searcher,
			analyzer,
			aggregator,
		),
		func(err error) {
			ticker.Stop()
			cancel()
		},
	)

	logger.Log(
		"status", "starting",
		"interval", conf.runInterval,
		"maxTweets", conf.maxTweets,
		"shutdownWait", conf.shutdownWait,
		"listening", conf.listenAddress,
		"projectID", conf.projectID,
	)

	if err := group.Run(); err != nil {
		logger.Log("status", "stopping", "err", err)
		fatal(logger, err)
	}

}

func runAnalysis(ctx context.Context, logger log.Logger, ticker *time.Ticker, searcher *centiment.Searcher, analyzer *centiment.Analyzer, aggregator *centiment.Aggregator) func() error {
	return func() error {
		// Trigger an immediate first run.
		now := make(chan struct{}, 1)
		now <- struct{}{}
		defer close(now)

		run := func() {
			logger.Log("state", "running")
			start := time.Now()

			searched := make(chan *centiment.SearchResult)
			analyzed := make(chan *centiment.AnalyzerResult)

			go searcher.Run(ctx, searched)
			go aggregator.Run(ctx, analyzed)
			analyzer.Run(ctx, searched, analyzed)

			logger.Log(
				"status", "finished",
				"duration", time.Since(start).String(),
			)
		}

		for {
			select {
			// TODO(matt): Condense into one case with custom ticker()
			case <-now:
				run()
			case <-ticker.C:
				run()
			case <-ctx.Done():
				ticker.Stop()
				return errors.Errorf("stopped analysis run: %s", ctx.Err())
			}
		}
	}
}

func signalHandler(ctx context.Context) func() error {
	return func() error {
		c := make(chan os.Signal, 1)
		signal.Notify(c, os.Interrupt, syscall.SIGTERM)

		select {
		case sig := <-c:
			return errors.Errorf("received signal: %s", sig)
		case <-ctx.Done():
			return errors.Errorf("cancelled: %s", ctx.Err())
		}
	}
}

func fatal(logger log.Logger, err error) {
	logger.Log(
		"status", "fatal",
		"err", err,
	)

	os.Exit(1)
	return
}
