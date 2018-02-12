package centiment

import (
	"context"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
)

// Aggregator aggregates results from an analysis run.
type Aggregator struct {
	logger log.Logger
	db     DB
}

// NewAggregator creates a new Aggregator: call Run to collect results and save
// them to the given DB.
func NewAggregator(logger log.Logger, db DB) (*Aggregator, error) {
	agg := &Aggregator{
		db:     db,
		logger: logger,
	}

	return agg, nil
}

// Run an aggregatation on the provided results.
func (ag *Aggregator) Run(ctx context.Context, results <-chan *AnalyzerResult) error {
	var sentiments = make(map[string]*Sentiment)

	// TODO(matt): Handle cancellation. Use for-select here with two cases.
	for res := range results {
		topic := res.SearchTerm.Topic
		if sentiments[topic] == nil {
			sentiments[topic] = &Sentiment{}
		}

		// Update the rolling aggregate for each topic.
		sentiments[topic] = ag.updateAggregate(
			res.Score,
			res.Magnitude,
			res.TweetID,
			sentiments[topic],
		)

		sentiments[topic].populateWithSearch(res.SearchTerm)
	}

	for topic, sentiment := range sentiments {
		sentiment.finalize()
		id, err := ag.db.SaveSentiment(ctx, *sentiment)
		if err != nil {
			// TODO(matt): Implement retry logic w/ back-off.
			ag.logger.Log(
				"err", errors.Wrap(err, "failed to save topic"),
				"topic", topic,
			)
			continue
		}

		ag.logger.Log(
			"state", "saved",
			"topic", sentiment.Topic,
			"slug", sentiment.Slug,
			"id", id,
			"score", sentiment.Score,
			"count", sentiment.Count,
			"stddev", sentiment.StdDev,
			"variance", sentiment.Variance,
		)
	}

	return nil
}

func (ag *Aggregator) updateAggregate(score float32, magnitude float32, tweetID int64, sentiment *Sentiment) *Sentiment {
	sentiment.Count++
	oldAverage := sentiment.Score
	sentiment.Score = updateAverage(score, sentiment.Score, sentiment.Count)
	sentiment.Variance = updateVariance(
		score,
		sentiment.Variance,
		oldAverage,
		sentiment.Score,
		sentiment.Count,
	)

	// Record the largest (newest) Tweet ID we've seen across our results for this
	// topic, as the checkpoint for future searches.
	if tweetID > sentiment.LastSeenID {
		sentiment.LastSeenID = tweetID
	}

	return sentiment
}

func updateAverage(value float32, currentAverage float64, count int64) float64 {
	return currentAverage + ((float64(value) - currentAverage) / float64(count))
}

func updateVariance(value float32, variance float64, oldAverage float64, newAverage float64, count int64) float64 {
	return variance + (float64(value)-oldAverage)*(float64(value)-newAverage)
}
