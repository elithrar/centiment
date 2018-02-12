package centiment

import (
	"context"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ChimeraCoder/anaconda"
	"github.com/go-kit/kit/log"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
	"golang.org/x/text/unicode/norm"
	languagepb "google.golang.org/genproto/googleapis/cloud/language/v1"
)

// SearchTerm represents the term or phrase to search for a given topic.
type SearchTerm struct {
	// The human-readable topic of the search.
	Topic string
	// The Twitter search query
	// Ref: https://developer.twitter.com/en/docs/tweets/search/guides/standard-operators
	Query string
}

func (st *SearchTerm) buildQuery() string {
	st.Query = strings.TrimSpace(st.Query)
	return st.Query
}

// SearchResult represents the result of a search against Twitter, and
// encapsulates a Tweet.
type SearchResult struct {
	searchTerm *SearchTerm
	tweetID    int64
	retweet    bool
	content    string
}

// sentimentRequest prepares a SearchResult for sentiment analysis.
func (s *SearchResult) sentimentRequest() *languagepb.AnalyzeSentimentRequest {
	return &languagepb.AnalyzeSentimentRequest{
		Document: &languagepb.Document{
			Source: &languagepb.Document_Content{
				Content: string(norm.NFC.Bytes([]byte(s.content))),
			},
			Type:     languagepb.Document_PLAIN_TEXT,
			Language: "en",
		},
	}
}

// Searcher is a worker pool that searches Twitter for the given
// set of search terms. Call NewSearcher to configure a new pool.
// Pools are safe to use concurrently.
//
// The "Run" method on Searcher should be used to begin a search.
type Searcher struct {
	twitterClient *anaconda.TwitterApi
	db            DB
	httpClient    *http.Client
	logger        log.Logger
	wg            sync.WaitGroup
	searchTerms   []*SearchTerm
	minResults    int
	maxAge        time.Duration
}

// NewSearcher creates a new Searcher with the given search terms. It will attempt to fetch minResults per search term and return tweets newer than maxAge.
func NewSearcher(logger log.Logger, terms []*SearchTerm, minResults int, maxAge time.Duration, client *anaconda.TwitterApi, db DB) (*Searcher, error) {
	if terms == nil || len(terms) < 1 {
		return nil, errors.New("searcher: terms must not be nil or empty")
	}

	// TODO(matt): Create validate() method on *SearchTerm type instead?
	for _, t := range terms {
		if t.Topic == "" {
			return nil, errors.New("searcher: search topics must not be empty")
		}

		if t.Query == "" {
			return nil, errors.New("searcher: search queries must not be empty")
		}
	}

	if minResults < 1 {
		return nil, errors.New("searcher: minResults must be > 0")
	}

	sr := &Searcher{
		twitterClient: client,
		httpClient:    &http.Client{},
		db:            db,
		maxAge:        maxAge,
		minResults:    minResults,
		logger:        logger,
		searchTerms:   terms,
	}

	sr.twitterClient.HttpClient = sr.httpClient

	_, err := sr.twitterClient.VerifyCredentials()
	if err != nil {
		return nil, errors.Wrap(err, "could not authenticate to Twitter API")
	}

	return sr, nil
}

// Run performs a concurrent search against the configured terms, and returns
// results onto the provided searched channel.
//
// Run returns when searches have completed, and can be cancelled by wrapping
// the provided context with context.WithCancel and calling the provided
// CancelFunc.
func (sr *Searcher) Run(ctx context.Context, searched chan<- *SearchResult) error {
	sr.wg.Add(len(sr.searchTerms))
	for _, term := range sr.searchTerms {
		go sr.search(ctx, *term, searched)
	}

	sr.wg.Wait()
	close(searched)

	return nil
}

func (sr *Searcher) getLastSeenID(ctx context.Context, st SearchTerm) (int64, error) {
	topicSlug := slug.Make(st.Topic)
	sentiments, err := sr.db.GetSentimentsBySlug(
		ctx,
		topicSlug,
		1,
	)
	if err != nil {
		return 0, err
	}

	if len(sentiments) != 1 {
		return 0, errors.Errorf("ambiguous number of sentiments returned: want %d, got %d", 1, len(sentiments))
	}

	return sentiments[0].LastSeenID, nil
}

func (sr *Searcher) search(ctx context.Context, st SearchTerm, searched chan<- *SearchResult) {
	defer sr.wg.Done()

	fromID, err := sr.getLastSeenID(ctx, st)
	if err != nil {
		// Log the error, but proceed without the checkpoint.
		sr.logger.Log("err", err, "topic", st.Topic)
	}

	params := url.Values{}
	params.Set("result_type", "recent")
	params.Set("lang", "en")
	if sr.minResults > 100 {
		params.Set("count", "100")
	} else {
		params.Set("count", strconv.Itoa(sr.minResults))

	}

	term := st.buildQuery()
	sr.logger.Log(
		"status", "searching",
		"topic", st.Topic,
		"query", st.Query,
		"fromID", fromID,
	)

	var (
		collected int // Total tweets collected
		seen      int // Total tweets seen
		// Acts as our paginaton cursor. We use this to fetch the next set (older) results.
		// Ref: https://developer.twitter.com/en/docs/tweets/timelines/guides/working-with-timelines
		cursor int64 = math.MaxInt64
	)

	// If we see 3x the minimum result count, and have not collected sufficient
	// results, we give up the search until the next run. This may occur when we
	// are attempting to fetch too many tweets at short intervals for a search
	// query with minimal results.
	for (collected < sr.minResults) || (seen >= sr.minResults*3) {
		select {
		// Cancel before the next fetch, but still allow any fetched tweets to be
		// processed.
		case <-ctx.Done():
			sr.logger.Log("status", "closing", "err", ctx.Err())
			return
		default:
		}

		// Only fetch tweets older than our cursor
		params.Set("max_id", strconv.FormatInt(cursor-1, 10))
		// Don't fetch tweets older than since_id
		params.Set("since_id", strconv.FormatInt(fromID, 10))

		sr.twitterClient.ReturnRateLimitError(true)
		resp, err := sr.twitterClient.GetSearch(term, params)
		if err != nil {
			// TODO(matt): Inspect & log rate limit errors.
			// TODO(matt): Implement retry logic.
			sr.logger.Log("err", err, "msg", "Twitter API error")
			return
		}

		for _, status := range resp.Statuses {
			// Track the oldest (lowest) tweet ID as our pagination cursor.
			if cursor > status.Id {
				cursor = status.Id
			}

			t, err := status.CreatedAtTime()
			if err != nil {
				seen++
				continue
			}

			// Skip "old" results to ensure relevance.
			if time.Since(t) > sr.maxAge {
				seen++
				continue
			}

			var retweet bool
			if status.RetweetedStatus != nil {
				retweet = true
			}

			s := &SearchResult{
				searchTerm: &st,
				tweetID:    status.Id,
				retweet:    retweet,
				content:    status.Text,
			}

			searched <- s
			collected++
			seen++
		}
	}
}
