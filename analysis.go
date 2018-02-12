package centiment

import (
	"context"
	"net/http"
	"sync"

	nl "cloud.google.com/go/language/apiv1"
	"github.com/go-kit/kit/log"
)

// Analyzer holds the configuration for running analyses against a Natural
// Language API. An Analyzer should only be initialized via NewAnalyzer.
type Analyzer struct {
	nlClient   *nl.Client
	httpClient *http.Client
	logger     log.Logger
	wg         sync.WaitGroup
	numWorkers int
}

// AnalyzerResult is the result from natural language analysis of a tweet.
type AnalyzerResult struct {
	TweetID    int64
	Score      float32
	Magnitude  float32
	SearchTerm *SearchTerm
}

// NewAnalyzer instantiates an Analyzer. Call the Run method to start an analysis.
func NewAnalyzer(logger log.Logger, client *nl.Client, numWorkers int) (*Analyzer, error) {
	ap := &Analyzer{
		nlClient:   client,
		httpClient: &http.Client{},
		logger:     logger,
		numWorkers: numWorkers,
	}

	return ap, nil
}

// Run passes the values from searched to the Natural Language API, performs
// analysis concurrently, and returns the results on the analyzed channel.
//
// Run returns when analyses have completed, and can be cancelled by wrapping
// the provided context with context.WithCancel and calling the provided
// CancelFunc.
func (az *Analyzer) Run(ctx context.Context, searched <-chan *SearchResult, analyzed chan<- *AnalyzerResult) error {
	az.wg.Add(az.numWorkers)
	for i := 0; i < az.numWorkers; i++ {
		// Spawn worker, pass context.
		go az.analyze(ctx, searched, analyzed)
	}

	// We block until we've processed all results.
	az.wg.Wait()
	close(analyzed)

	return nil
}

func (az *Analyzer) analyze(ctx context.Context, searched <-chan *SearchResult, analyzed chan<- *AnalyzerResult) {
	defer az.wg.Done()

	for {
		select {
		case st, ok := <-searched:
			if !ok {
				return
			}

			resp, err := az.nlClient.AnalyzeSentiment(ctx, st.sentimentRequest())
			if err != nil {
				az.logger.Log(
					"err", err,
					"topic", st.searchTerm.Topic,
					"content", st.content,
				)
				// TODO(matt): Implement retry logic.
				// TODO(matt): Count errors & fail closed if errors approaches some % of
				// seen results
				continue
			}

			result := &AnalyzerResult{
				TweetID:    st.tweetID,
				Score:      resp.DocumentSentiment.GetScore(),
				Magnitude:  resp.DocumentSentiment.GetMagnitude(),
				SearchTerm: st.searchTerm,
			}

			analyzed <- result

		case <-ctx.Done():
			az.logger.Log("status", "closing", "err", ctx.Err())
			return
		}
	}

}
