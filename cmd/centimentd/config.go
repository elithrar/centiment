package main

import (
	"os"
	"time"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
	"github.com/alecthomas/kingpin"
	"github.com/elithrar/centiment"
	"github.com/pkg/errors"
)

type config struct {
	accessSecret     string
	accessToken      string
	consumerKey      string
	consumerSecret   string
	hostname         string
	listenAddress    string
	maxTweets        int
	numWorkers       int
	projectID        string
	runInterval      time.Duration
	searchConfigPath string
	shutdownWait     time.Duration
}

func parseConfig() (*config, error) {
	cmd := kingpin.New("centiment", "⚡️ Centiment is a service that performs sentiment analysis of tweets using Google's Natural Language APIs.")
	conf := &config{}

	// Application config
	cmd.Flag("listen", "The address (IP:port) to listen on").Default("0.0.0.0:8080").Envar("CENTIMENT_ADDRESS").StringVar(&conf.listenAddress)
	cmd.Flag("max-tweets", "The maximum number of tweets to fetch per given topic").Default("50").Envar("CENTIMENT_MAX_TWEETS").IntVar(&conf.maxTweets)
	cmd.Flag("analysis-workers", "The number of workers used to process requests against the Natural Language API").Default("10").Envar("CENTIMENT_ANALYSIS_WORKERS").IntVar(&conf.numWorkers)
	cmd.Flag("project-id", "The Google Cloud project ID to use for Firestore").Required().Envar("CENTIMENT_PROJECT_ID").StringVar(&conf.projectID)
	cmd.Flag("run-interval", "How often an analysis run occurs").Default("10m").Envar("CENTIMENT_RUN_INTERVAL").DurationVar(&conf.runInterval)
	cmd.Flag("search-config", "The path to the TOML file containing search terms").Default("./search.toml").Envar("CENTIMENT_SEARCH_CONFIG").StringVar(&conf.searchConfigPath)
	cmd.Flag("hostname", "The hostname to serve requests for").Default("centiment.questionable.services").Envar("CENTIMENT_HOSTNAME").StringVar(&conf.hostname)
	cmd.Flag("shutdown-wait", "The grace period to allow for finishing any ongoing analysis before terminating on SIGINT").Default("10s").Envar("CENTIMENT_SHUTDOWN_WAIT").DurationVar(&conf.shutdownWait)

	// Twitter keys
	cmd.Flag("twitter-consumer-key", "The Twitter consumer API key").Required().Envar("TWITTER_CONSUMER_KEY").StringVar(&conf.consumerKey)
	cmd.Flag("twitter-consumer-secret", "The Twitter consumer API secret").Required().Envar("TWITTER_CONSUMER_SECRET").StringVar(&conf.consumerSecret)
	cmd.Flag("twitter-access-token", "The Twitter client access token").Required().Envar("TWITTER_ACCESS_TOKEN").StringVar(&conf.accessToken)
	cmd.Flag("twitter-access-secret", "The Twitter client access token").Required().Envar("TWITTER_ACCESS_SECRET").StringVar(&conf.accessSecret)

	_, err := cmd.Parse(os.Args[1:])
	if err != nil {
		return nil, err
	}

	return conf, nil
}

type searchConfig struct {
	SearchTerms []*centiment.SearchTerm `toml:"search"`
}

func parseSearchTerms(fpath string) ([]*centiment.SearchTerm, error) {
	var sc *searchConfig

	if _, err := toml.DecodeFile(fpath, &sc); err != nil {
		return nil, err
	}

	for _, term := range sc.SearchTerms {
		if count := utf8.RuneCountInString(term.Topic); count < 3 {
			return nil, errors.Errorf("search topics must be > 3 characters long: %q is only %d characters", term.Topic, count)
		}

		if count := utf8.RuneCountInString(term.Query); count < 3 {
			return nil, errors.Errorf("search queries must be > 3 characters long: %q is only %d characters", term.Query, count)
		}
	}

	return sc.SearchTerms, nil
}
