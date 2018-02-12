package centiment

import (
	// Imports the Google Cloud Natural Language API client package.
	"context"
	"math"
	"strings"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/gosimple/slug"
	"github.com/pkg/errors"
)

var (

	// ErrNoResultsFound is returned when a DB cannot find a result in the store.
	ErrNoResultsFound = errors.New("store: no results found")
	// ErrInvalidSlug is returned when a URL slug does not match expected slug format (via slug.IsSlug)
	ErrInvalidSlug = errors.New("store: bad slug format")
)

// DB represents a database for storing & retrieving Sentiments.
type DB interface {
	SaveSentiment(ctx context.Context, sentiment Sentiment) (string, error)
	GetSentimentByID(ctx context.Context, id string) (*Sentiment, error)
	GetSentimentsBySlug(ctx context.Context, slug string, limit int) ([]*Sentiment, error)
	GetSentimentsByTopic(ctx context.Context, topic string, limit int) ([]*Sentiment, error)
}

// Firestore is an implementation of DB that uses Google Cloud Firestore.
type Firestore struct {
	Store *firestore.Client
	// The name of the collection.
	CollectionName string
}

// Sentiment represents the aggregated result of performing sentiment analysis
// against a number (Count) of tweets for a given topic.
type Sentiment struct {
	ID         string    `json:"id" firestore:"id,omitempty"`
	Topic      string    `json:"topic" firestore:"topic"`
	Slug       string    `json:"slug" firestore:"slug"`
	Query      string    `json:"query" firestore:"query"`
	Count      int64     `json:"count" firestore:"count"`
	Score      float64   `json:"score" firestore:"score"`
	StdDev     float64   `json:"stdDev" firestore:"stdDev"`
	Variance   float64   `json:"variance" firestore:"variance"`
	FetchedAt  time.Time `json:"fetchedAt" firestore:"fetchedAt"`
	LastSeenID int64     `json:"-" firestore:"lastSeenID"`
}

// populateWithSearch sets the search-related metadata on the Sentiment.
func (s *Sentiment) populateWithSearch(st *SearchTerm) {
	s.Topic = strings.TrimSpace(strings.ToLower(st.Topic))
	// Leave the query as-is (the search query exactly)
	s.Query = st.Query
}

// finalize the Sentiment for saving: finalize aggregates & sets the timestamp.
func (s *Sentiment) finalize() {
	s.Variance = s.Variance / float64((s.Count - 1))
	s.StdDev = math.Sqrt(s.Variance)
	s.FetchedAt = time.Now().UTC()
	s.Slug = slug.Make(s.Topic)
}
